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
	"sort"
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
	"world":       true,
	"opinion":     true,
	"experience":  true,
	"observation": true,
	"task":        true,
}

// validRelationTypes is the allowlist of relation labels the LLM
// extractor produces. Keeping the set small and descriptive prevents
// graph pollution from one-off relation labels like "thinks_about_v2".
var validRelationTypes = map[string]bool{
	"prefers":      true,
	"uses":         true,
	"mentions":     true,
	"related_to":   true,
	"part_of":      true,
	"causes":       true,
	"contradicts":  true,
	"blocked_by":   true,
	"recovers_via": true,
}

// filterEntities drops entities whose category is outside the allowlist
// and drops relations whose relation_type is outside the allowlist.
// Empty/whitespace-only relations are also dropped. Surviving entities
// retain a nil-clamped Relations slice so JSON output stays stable.
//
// The category and relation-type maps are passed in (rather than
// read from package-level vars) so Config-driven extras compose
// cleanly with the package-level defaults. nil maps count as
// "no entries allowed" — callers should always pass a non-nil
// map; the nil check is defensive so tests that forge inputs
// cannot panic the production code path.
//
// Allocation: walks the input twice — first to count the survivors
// (one map lookup per entity, no allocations), then to copy them
// into a slice with exact pre-counted capacity. When validCount ==
// len(in) the new slice is the same size as the input, so pass-through
// behaviour is preserved; when validCount < len(in) we save the
// wasted capacity that the previous `make([]ExtractedEntity, 0, len(in))`
// reserved but never used.
func filterEntities(in []ExtractedEntity, validCats, validRels map[string]bool) []ExtractedEntity {
	validCount := 0
	for _, e := range in {
		if validCats[e.Category] {
			validCount++
		}
	}
	out := make([]ExtractedEntity, 0, validCount)
	for _, e := range in {
		if !validCats[e.Category] {
			continue
		}
		e.Relations = filterRelations(e.Relations, validRels)
		out = append(out, e)
	}
	return out
}

// filterRelations applies TWO independent rules per incoming relation:
// (1) TargetID must be non-empty (defensive against dangling edges that
// SQLite's INSERT OR IGNORE would silently accept), and
// (2) RelationType must be in validRels (graph-pollution guard).
// Either failure drops the relation; surviving relations retain their
// declared target_id untouched (no cross-entity resolution happens here).
// validRels is passed in to compose with Config.Extras at the call
// site; nil maps count as "no entries allowed".
//
// Allocation: pre-counts the survivors before allocating, so make is
// called with exact capacity and append never re-grows the backing
// array. The previous `make([]Relation, 0, len(in))` reserved more
// than necessary on filtered batches.
func filterRelations(in []Relation, validRels map[string]bool) []Relation {
	validCount := 0
	for _, r := range in {
		if r.TargetID != "" && validRels[r.RelationType] {
			validCount++
		}
	}
	out := make([]Relation, 0, validCount)
	for _, r := range in {
		if r.TargetID == "" || !validRels[r.RelationType] {
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

	// validCategories and validRelationTypes are the per-extractor
	// allowlists used by filterEntities / filterRelations and by
	// buildSystemPrompt to compose the system prompt dynamically
	// from Config-derived maps. nil maps are tolerated at the
	// constructor (replaced with empty ones) but downstream code
	// paths still treat them as "nothing allowed" — they should
	// be non-empty in production.
	validCategories    map[string]bool
	validRelationTypes map[string]bool

	// client owns the HTTP transport so retries reuse the TCP connection
	// and per-request timeout is enforced consistently.
	client *http.Client
}

func NewOllamaLLMExtractor(baseURL, model string, temperature float32, timeout time.Duration, validCategories, validRelationTypes map[string]bool) *OllamaLLMExtractor {
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
	if validCategories == nil {
		validCategories = map[string]bool{}
	}
	if validRelationTypes == nil {
		validRelationTypes = map[string]bool{}
	}
	return &OllamaLLMExtractor{
		BaseURL:            baseURL,
		Model:              model,
		Temperature:        temperature,
		validCategories:    validCategories,
		validRelationTypes: validRelationTypes,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// knownCategoryDescriptions pairs the four default categories with
// one-line semantic descriptions that are baked into the system
// prompt. Operator-supplied extras (via Config.ExtraCategories) are
// emitted as bare names without descriptions — operators that want
// semantic context for extras should include it in the prompt at a
// higher layer or provide a `description` field in a future config
// extension.
var knownCategoryDescriptions = map[string]string{
	"world":       "facts, definitions, objective knowledge",
	"opinion":     "preferences, beliefs, subjective views",
	"experience":  "past events, interactions, what happened",
	"observation": "patterns noticed, anomalies, insights",
	"task":        "actionable work items, steps, to-dos with status tracking",
}

// buildSystemPrompt composes the system prompt dynamically from the
// merged category and relation-type maps. Operators adding an extra
// via Config.ExtraCategories / ExtraRelationTypes see the model emit
// the new tag/relation after a single restart — no source rebuild
// required.
//
// Keys are sorted alphabetically via sortedKeys so the rendered
// prompt is deterministic across runs (helps prompt-cache hit rate
// and keeps snapshot tests stable).
//
// Schema hint keeps a `<one of: a, b, ...>` concrete example —
// placeholders like `<one-of-allowed>` are interpreted less reliably
// by some LLMs, so the schema hint mirrors a small slice of the
// rendered list verbatim.
//
// Accuracy assumption: the dynamic prompt is treated as equivalent
// to the previously-hardcoded prompt. This has not been benchmarked
// against real Ollama/OpenAI traffic; a regression in extraction
// quality after this change should be investigated as either a
// llm-side prompt drift or a here-side ordering glitch.
func buildSystemPrompt(validCategories, validRelationTypes map[string]bool) string {
	cats := sortedKeys(validCategories)
	rels := sortedKeys(validRelationTypes)

	categoryLines := make([]string, 0, len(cats))
	for _, c := range cats {
		if desc, ok := knownCategoryDescriptions[c]; ok {
			categoryLines = append(categoryLines, "- "+c+": "+desc)
		} else {
			categoryLines = append(categoryLines, "- "+c)
		}
	}

	// Schema hint uses up to 3 examples each — small enough to keep
	// the literal compact, large enough that the LLM sees three
	// independent tokens rather than a one-token fan-in.
	catExamples := cats
	if len(catExamples) > 3 {
		catExamples = catExamples[:3]
	}
	relExamples := rels
	if len(relExamples) > 3 {
		relExamples = relExamples[:3]
	}

	return `You are a knowledge extraction assistant. Extract entities and relations from dialog text.

Categories (use EXACTLY these strings, no others):
` + strings.Join(categoryLines, "\n") + `

Allowed relation_type values (use EXACTLY these strings, no others):
"` + strings.Join(rels, `", "`) + `"

Rules:
1. Extract atomic, self-contained entities
2. Each entity needs a unique kebab-case id
3. Relations connect entities with one of the allowed relation_type strings
4. Only include clear, useful knowledge
5. Return ONLY valid JSON matching this exact schema:
{"entities":[{"id":"string","category":"<one of: ` + strings.Join(catExamples, ", ") + `>","content":"string","relations":[{"target_id":"string","relation_type":"<one of: ` + strings.Join(relExamples, ", ") + `>"}]}]}

Dialog:`
}

// sortedKeys returns the keys of m sorted alphabetically. Returns
// an empty slice for nil/empty maps so the join renders "" without
// special-casing at the prompt site.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// stripMarkdownCodeFence strips a markdown ```json or ``` fence around
// JSON content. Models frequently wrap JSON in such fences despite
// "return only JSON" instructions; without stripping, json.Unmarshal
// fails on the leading backtick.
//
// Idempotent on already-clean JSON: when no fence is present, the
// input is returned verbatim (trimmed). The `json` tag inside the
// fence is matched case-insensitively.
//
// Behaviour:
//   - `{"a":1}`                                -> `{"a":1}`
//   - "```json\n{\"a\":1}\n```"                -> `{"a\":1}`
//   - "```\n{\"a\":1}\n```"                    -> `{"a\":1}`
//   - "```JSON\n{\"a\":1}\n```"                -> `{"a\":1}`
//   - "preamble ```json\n{...}\n``` postamble" -> `{...}`
//   - unclosed "```json\n{\"a\":1}"            -> `{"a\":1}` (best effort)
func stripMarkdownCodeFence(s string) string {
	openIdx, tagLen := findCodeFence(s)
	if openIdx == -1 {
		return strings.TrimSpace(s)
	}
	innerStart := openIdx + tagLen
	for innerStart < len(s) && isFenceWS(s[innerStart]) {
		innerStart++
	}
	rest := s[innerStart:]
	closeIdx := strings.Index(rest, "```")
	if closeIdx == -1 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:closeIdx])
}

// findCodeFence returns the index of an opening ``` fence, preferring
// ```json over plain ```. Returns (-1, 0) when no fence is present.
func findCodeFence(s string) (int, int) {
	idx := strings.Index(s, "```")
	if idx == -1 {
		return -1, 0
	}
	after := idx + 3
	if after+4 <= len(s) && strings.EqualFold(s[after:after+4], "json") {
		return idx, 7
	}
	return idx, 3
}

func isFenceWS(c byte) bool {
	return c == '\n' || c == '\r' || c == ' ' || c == '\t'
}

// OpenAILLMExtractor extracts entities via the OpenAI Chat Completions API.
type OpenAILLMExtractor struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float32

	validCategories    map[string]bool
	validRelationTypes map[string]bool

	client *http.Client
}

func NewOpenAILLMExtractor(baseURL, apiKey, model string, temperature float32, timeout time.Duration, validCategories, validRelationTypes map[string]bool) *OpenAILLMExtractor {
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
	if validCategories == nil {
		validCategories = map[string]bool{}
	}
	if validRelationTypes == nil {
		validRelationTypes = map[string]bool{}
	}
	return &OpenAILLMExtractor{
		BaseURL:            baseURL,
		APIKey:             apiKey,
		Model:              model,
		Temperature:        temperature,
		validCategories:    validCategories,
		validRelationTypes: validRelationTypes,
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
	Model          string              `json:"model"`
	Messages       []openAIChatMessage `json:"messages"`
	Temperature    float32             `json:"temperature"`
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

	// System prompt is composed from the per-extractor validCategories
	// and validRelationTypes maps so Config.Extras modify what the
	// model sees after a single restart — no source rebuild required.
	systemPrompt := buildSystemPrompt(e.validCategories, e.validRelationTypes)

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
	// Defensive: even with response_format=json_object, some
	// fine-tuned variants or downgraded models emit markdown
	// fences when chat drift confuses them. Strip before parsing
	// so transient fence wrapping doesn't cascade to the caller
	// as a parse error.
	content = stripMarkdownCodeFence(content)
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

	result.Entities = filterEntities(result.Entities, e.validCategories, e.validRelationTypes)

	return &result, nil
}

func (e *OllamaLLMExtractor) ExtractEntities(ctx context.Context, dialog string) (*ExtractionResult, error) {

	systemPrompt := buildSystemPrompt(e.validCategories, e.validRelationTypes)

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
	// Strip markdown fences that Ollama emits despite format=json.
	// Without this, json.Unmarshal fails on the leading backtick and
	// every Ollama response that wraps its JSON in ``` ... ``` would
	// surface as "parse error" — even when the inner JSON is valid.
	content = stripMarkdownCodeFence(content)
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

	result.Entities = filterEntities(result.Entities, e.validCategories, e.validRelationTypes)

	return &result, nil
}
