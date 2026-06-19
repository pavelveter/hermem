package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Relation is a typed edge between two extracted entities.
type Relation struct {
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

// ExtractedEntity is a single knowledge unit returned by an LLM extractor.
type ExtractedEntity struct {
	ID        string     `json:"id"`
	Category  string     `json:"category"`
	Content   string     `json:"content"`
	Relations []Relation `json:"relations"`
}

// ExtractionResult is the structured payload returned by an LLMExtractor.
type ExtractionResult struct {
	Entities []ExtractedEntity `json:"entities"`
}

// LLMExtractor turns dialog text into structured entities.
type LLMExtractor interface {
	ExtractEntities(dialog string) (*ExtractionResult, error)
}

// Ollama chat-call tuning. Held in package-level consts so the values
// compile into the binary for predictability; deployments that need
// different timings should fork or wrap the extractor.
const (
	// ollamaRequestTimeout caps a single HTTP attempt. Combined with the
	// retry budget it bounds the worst-case ExtractEntities latency.
	ollamaRequestTimeout = 10 * time.Second

	// ollamaRetries is the total number of chat attempts (first try +
	// retries). 3 keeps a sync HTTP path responsive on transient Ollama
	// hiccups without burning a request budget on persistent failures.
	ollamaRetries = 3

	// ollamaBackoffBase is the first retry sleep; doubles every attempt
	// (200ms, 400ms, 800ms) up to ollamaBackoffMax.
	ollamaBackoffBase = 200 * time.Millisecond
	ollamaBackoffMax  = 2 * time.Second
)

// validCategories is the allowlist of entity categories the LLM
// extractor produces. The system prompt mirrors these so the model emits
// only these values; runtime filtering is the safety net for prompts
// that ignore the schema.
var validCategories = map[string]bool{
	"world":      true,
	"opinion":    true,
	"experience": true,
	"observation": true,
}

// validRelationTypes is the allowlist of relation labels the LLM
// extractor produces. Keeping the set small and descriptive prevents
// graph pollution from one-off relation labels like "thinks_about_v2".
var validRelationTypes = map[string]bool{
	"prefers":     true,
	"uses":        true,
	"mentions":    true,
	"related_to":  true,
	"part_of":     true,
	"causes":      true,
	"contradicts": true,
}

// filterEntities drops entities whose category is outside the allowlist
// and drops relations whose relation_type is outside the allowlist.
// Empty/whitespace-only relations are also dropped. Surviving entities
// retain a nil-clamped Relations slice so JSON output stays stable.
func filterEntities(in []ExtractedEntity) []ExtractedEntity {
	out := make([]ExtractedEntity, 0, len(in))
	for _, e := range in {
		if !validCategories[e.Category] {
			continue
		}
		e.Relations = filterRelations(e.Relations)
		out = append(out, e)
	}
	return out
}

// filterRelations applies TWO independent rules per incoming relation:
// (1) TargetID must be non-empty (defensive against dangling edges that
// SQLite's INSERT OR IGNORE would silently accept), and
// (2) RelationType must be in validRelationTypes (graph-pollution guard).
// Either failure drops the relation; surviving relations retain their
// declared target_id untouched (no cross-entity resolution happens here).
func filterRelations(in []Relation) []Relation {
	out := make([]Relation, 0, len(in))
	for _, r := range in {
		if r.TargetID == "" || !validRelationTypes[r.RelationType] {
			continue
		}
		out = append(out, r)
	}
	return out
}

type OllamaLLMExtractor struct {
	BaseURL string
	Model   string

	// client owns the HTTP transport so retries reuse the TCP connection
	// and per-request timeout is enforced consistently.
	client *http.Client
}

func NewOllamaLLMExtractor(baseURL, model string) *OllamaLLMExtractor {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "qwen2.5-coder:7b"
	}
	return &OllamaLLMExtractor{
		BaseURL: baseURL,
		Model:   model,
		client: &http.Client{
			Timeout: ollamaRequestTimeout,
		},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Format   string         `json:"format,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
}

// chat performs one POST to /api/chat with retries on transient failures.
// Only network errors and HTTP 5xx are retried; JSON/content errors
// return immediately because they won't change on retry.
func (e *OllamaLLMExtractor) chat(ctx context.Context, req *chatRequest) (*chatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("extractor: marshal chat request: %w", err)
	}

	url := e.BaseURL + "/api/chat"

	var lastErr error
	backoff := ollamaBackoffBase
	attemptsUsed := 0
	defer func() {
		// Single-event post-call summary at Debug (level-filter as
		// throttle per TODO §5.3). Replaces the per-attempt loop log
		// that would emit one line per retry attempt.
		outcome := "ok"
		if lastErr != nil {
			outcome = "error"
		}
		slog.Debug("ollama call complete",
			"event", "ollama_call",
			"model", e.Model,
			"attempts_used", attemptsUsed,
			"outcome", outcome,
		)
	}()
	for attempt := 1; attempt <= ollamaRetries; attempt++ {
		attemptsUsed = attempt
		if attempt > 1 {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			backoff *= 2
			if backoff > ollamaBackoffMax {
				backoff = ollamaBackoffMax
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("extractor: build chat request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := e.client.Do(httpReq)
		if err != nil {
			// Network/timeout: surface as retryable.
			lastErr = err
			slog.Warn("ollama attempt failed, will retry",
				"model", e.Model,
				"attempt", attempt,
				"err", err.Error(),
			)
			continue
		}

		if resp.StatusCode >= 500 {
			// Drain body before closing so the connection can be reused.
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("extractor: ollama HTTP %d", resp.StatusCode)
			slog.Warn("ollama returned 5xx, will retry",
				"model", e.Model,
				"attempt", attempt,
				"status", resp.StatusCode,
			)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("extractor: ollama HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
		}

		var cr chatResponse
		if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("extractor: decode chat response: %w", err)
		}
		if err := resp.Body.Close(); err != nil {
			return nil, fmt.Errorf("extractor: close chat response: %w", err)
		}
		return &cr, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("extractor: ollama chat failed after %d attempts: %w", ollamaRetries, lastErr)
	}
	return nil, fmt.Errorf("extractor: ollama chat failed after %d attempts", ollamaRetries)
}

// truncate caps a string for inclusion in error messages and log records.
// Returns "..." suffix when truncation actually happens so callers can
// tell a clipped string from a short one.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...<truncated>"
}

func (e *OllamaLLMExtractor) ExtractEntities(dialog string) (*ExtractionResult, error) {
	// Hard overall budget so a stuck Ollama can't pin the HTTP server
	// (or MemoryWorker) on a single bad request.
	totalBudget := ollamaRequestTimeout*time.Duration(ollamaRetries) + ollamaBackoffMax*time.Duration(ollamaRetries-1)
	ctx, cancel := context.WithTimeout(context.Background(), totalBudget)
	defer cancel()

	systemPrompt := `You are a knowledge extraction assistant. Extract entities and relations from dialog text.

Categories (use EXACTLY these strings, no others):
- world: facts, definitions, objective knowledge
- opinion: preferences, beliefs, subjective views
- experience: past events, interactions, what happened
- observation: patterns noticed, anomalies, insights

Allowed relation_type values (use EXACTLY these strings, no others):
"prefers", "uses", "mentions", "related_to", "part_of", "causes", "contradicts"

Rules:
1. Extract atomic, self-contained entities
2. Each entity needs a unique kebab-case id
3. Relations connect entities with one of the allowed relation_type strings
4. Only include clear, useful knowledge
5. Return ONLY valid JSON matching this exact schema:
{"entities":[{"id":"string","category":"world|opinion|experience|observation","content":"string","relations":[{"target_id":"string","relation_type":"string"}]}]}

Dialog:`

	req := chatRequest{
		Model: e.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: dialog},
		},
		Stream: false,
		Format: "json",
		Options: map[string]any{
			"temperature": 0.1,
		},
	}

	chatResp, err := e.chat(ctx, &req)
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(chatResp.Message.Content)
	if content == "" {
		return &ExtractionResult{Entities: []ExtractedEntity{}}, nil
	}

	var result ExtractionResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// LLM output is expected to be JSON. Surface the parse failure so the
		// ingester (caller of ExtractEntities) can decide policy. Truncate
		// the raw content to avoid unbounded error strings in HTTP responses
		// and log records.
		return nil, fmt.Errorf("extractor: parse LLM JSON response: %w (raw content: %s)", err, truncate(content, 200))
	}

	if result.Entities == nil {
		result.Entities = []ExtractedEntity{}
	}

	result.Entities = filterEntities(result.Entities)

	return &result, nil
}
