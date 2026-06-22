package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	ExtractEntities(ctx context.Context, dialog string) (*ExtractionResult, error)
}

// Ollama chat-call tuning. Held in package-level consts so the values
// compile into the binary for predictability; deployments that need
// different timings should fork or wrap the extractor.
const (
	// ollamaRequestTimeout caps a single HTTP attempt. Combined with the
	// retry budget it bounds the worst-case ExtractEntities latency.
	ollamaRequestTimeout = 300 * time.Second

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
	BaseURL     string
	Model       string
	Temperature float32

	// client owns the HTTP transport so retries reuse the TCP connection
	// and per-request timeout is enforced consistently.
	client *http.Client
}

func NewOllamaLLMExtractor(baseURL, model string, temperature float32, timeout time.Duration) *OllamaLLMExtractor {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "qwen2.5-coder:7b"
	}
	if temperature == 0 {
		temperature = 0.1
	}
	if timeout <= 0 {
		timeout = ollamaRequestTimeout
	}
	return &OllamaLLMExtractor{
		BaseURL:     baseURL,
		Model:       model,
		Temperature: temperature,
		client: &http.Client{
			Timeout: timeout,
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

// ollamaDoAttempt performs one HTTP POST to /api/chat and returns the
// decoded response. The response body is always closed before return.
// Returns nil response + retryable error for HTTP 5xx (caller retries);
// non-retryable error for 4xx / decode failures.
func (e *OllamaLLMExtractor) ollamaDoAttempt(ctx context.Context, url string, body []byte) (*chatResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 500 {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}
	return &cr, nil
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

		cr, err := e.ollamaDoAttempt(ctx, url, body)
		if err != nil {
			var httpErr *httpStatusError
			if errors.As(err, &httpErr) {
				lastErr = err
				slog.Warn("ollama returned 5xx, will retry",
					"model", e.Model,
					"attempt", attempt,
					"status", httpErr.code,
				)
				continue
			}
			return nil, fmt.Errorf("extractor: %w", err)
		}
		return cr, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("extractor: ollama chat failed after %d attempts: %w", ollamaRetries, lastErr)
	}
	return nil, fmt.Errorf("extractor: ollama chat failed after %d attempts", ollamaRetries)
}

// httpStatusError wraps an HTTP status code for retry decisions.
// chat retry loops distinguish 5xx (retryable) from 4xx (fatal) via
// errors.As on this type.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

// truncate caps a string for inclusion in error messages and log records.
// Returns "..." suffix when truncation actually happens so callers can
// tell a clipped string from a short one.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "...<truncated>"
}

// OpenAILLMExtractor extracts entities via the OpenAI Chat Completions API.
type OpenAILLMExtractor struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float32

	client *http.Client
}

func NewOpenAILLMExtractor(baseURL, apiKey, model string, temperature float32, timeout time.Duration) *OpenAILLMExtractor {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	if temperature == 0 {
		temperature = 0.1
	}
	if timeout <= 0 {
		timeout = ollamaRequestTimeout
	}
	return &OpenAILLMExtractor{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       model,
		Temperature: temperature,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float32             `json:"temperature"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

type openAIChatChoice struct {
	Message openAIChatMessage `json:"message"`
}

type openAIChatResponse struct {
	Choices []openAIChatChoice `json:"choices"`
}

// openaiDoAttempt performs one HTTP POST to /chat/completions and
// returns the decoded response. The response body is always closed
// before return. Returns nil response + retryable error for HTTP 5xx.
func (e *OpenAILLMExtractor) openaiDoAttempt(ctx context.Context, url string, body []byte) (*openAIChatResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 500 {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}

	var cr openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	return &cr, nil
}

func (e *OpenAILLMExtractor) chat(ctx context.Context, req *openAIChatRequest) (*openAIChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("extractor: marshal openai request: %w", err)
	}

	url := e.BaseURL + "/chat/completions"

	var lastErr error
	backoff := ollamaBackoffBase
	attemptsUsed := 0
	defer func() {
		outcome := "ok"
		if lastErr != nil {
			outcome = "error"
		}
		slog.Debug("openai call complete",
			"event", "openai_call",
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

		cr, err := e.openaiDoAttempt(ctx, url, body)
		if err != nil {
			var httpErr *httpStatusError
			if errors.As(err, &httpErr) {
				lastErr = err
				slog.Warn("openai returned 5xx, will retry",
					"model", e.Model,
					"attempt", attempt,
					"status", httpErr.code,
				)
				continue
			}
			return nil, fmt.Errorf("extractor: %w", err)
		}
		return cr, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("extractor: openai chat failed after %d attempts: %w", ollamaRetries, lastErr)
	}
	return nil, fmt.Errorf("extractor: openai chat failed after %d attempts", ollamaRetries)
}

func (e *OpenAILLMExtractor) ExtractEntities(ctx context.Context, dialog string) (*ExtractionResult, error) {

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

	req := openAIChatRequest{
		Model: e.Model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: dialog},
		},
		Temperature: e.Temperature,
		ResponseFormat: &struct {
			Type string `json:"type"`
		}{Type: "json_object"},
	}

	chatResp, err := e.chat(ctx, &req)
	if err != nil {
		return nil, err
	}

	if len(chatResp.Choices) == 0 {
		return &ExtractionResult{Entities: []ExtractedEntity{}}, nil
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if content == "" {
		return &ExtractionResult{Entities: []ExtractedEntity{}}, nil
	}

	var result ExtractionResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("extractor: parse openai JSON response: %w (raw content: %s)", err, truncate(content, 200))
	}

	if result.Entities == nil {
		result.Entities = []ExtractedEntity{}
	}

	result.Entities = filterEntities(result.Entities)

	return &result, nil
}

func (e *OllamaLLMExtractor) ExtractEntities(ctx context.Context, dialog string) (*ExtractionResult, error) {

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
			"temperature": e.Temperature,
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
