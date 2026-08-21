package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pavelveter/hermem/pkg/spi"
)

// NoopReranker returns the input candidates unchanged — used when no reranker is configured.
type NoopReranker struct{}

func (r *NoopReranker) Rerank(_ context.Context, _ string, candidates []spi.Candidate) ([]spi.Candidate, error) {
	return candidates, nil
}

// OllamaReranker calls Ollama's /api/rerank endpoint; on failure it returns input unchanged.
//
// Graceful-degrade is enforced explicitly via `if err != nil { return facts, nil }`
// after every call site, so transport / decode / no-data outcomes all preserve
// the input ordering rather than surface an error to the retrieval pipeline.
type OllamaReranker struct {
	BaseURL string
	Model   string
	http    *httpClient
}

func NewOllamaReranker(baseURL, model string, timeout time.Duration) *OllamaReranker {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaReranker{
		BaseURL: baseURL,
		Model:   model,
		http:    newHTTPClient(baseURL, "", "ollama", model, timeout, RetryPolicy{MaxAttempts: 3}),
	}
}

func (r *OllamaReranker) Rerank(ctx context.Context, query string, candidates []spi.Candidate) ([]spi.Candidate, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}
	docs := make([]string, len(candidates))
	for i, c := range candidates {
		docs[i] = c.Text
	}
	body := map[string]interface{}{
		"model":     r.Model,
		"query":     query,
		"documents": docs,
	}
	var rr struct {
		Results []struct {
			Index int `json:"index"`
		} `json:"results"`
	}
	if err := r.http.doPOST(ctx, "/api/rerank", body, &rr); err != nil {
		return candidates, nil
	}
	reranked := make([]spi.Candidate, 0, len(candidates))
	for _, item := range rr.Results {
		if item.Index >= 0 && item.Index < len(candidates) {
			reranked = append(reranked, candidates[item.Index])
		}
	}
	if len(reranked) == 0 {
		return candidates, nil
	}
	return reranked, nil
}

// OpenAIReranker uses OpenAI chat completions with a relevance-ordering prompt.
// On any failure it returns the original ordering.
type OpenAIReranker struct {
	BaseURL string
	APIKey  string
	Model   string
	http    *httpClient
}

func NewOpenAIReranker(baseURL, model, key string, timeout time.Duration) *OpenAIReranker {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAIReranker{
		BaseURL: baseURL,
		APIKey:  key,
		Model:   model,
		http:    newHTTPClient(baseURL, key, "openai", model, timeout, RetryPolicy{MaxAttempts: 3}),
	}
}

func (r *OpenAIReranker) Rerank(ctx context.Context, query string, candidates []spi.Candidate) ([]spi.Candidate, error) {
	if len(candidates) <= 1 {
		return candidates, nil
	}
	var docList strings.Builder
	for i, c := range candidates {
		fmt.Fprintf(&docList, "%d. %s\n", i+1, c.Text)
	}
	// Force structured JSON output via response_format so we never need to
	// parse a free-form "3,1,2"-style response. Free-form parsing was
	// fragile (LLM adds prose, code fences, or skips numbers) and made
	// the reranker effectively a placebo in degraded cases.
	prompt := fmt.Sprintf(`Query: %s

Documents:
%s

Reorder the documents by relevance to the query. Return ONLY a JSON object with this exact shape: {"order": [3, 1, 2]} listing the document numbers (1-indexed) in relevance order, most relevant first. Do not add any prose, code fences, or extra keys.`, query, docList.String())
	body := map[string]interface{}{
		"model":           r.Model,
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0,
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := r.http.doPOST(ctx, "/chat/completions", body, &cr); err != nil {
		return candidates, nil
	}
	if len(cr.Choices) == 0 {
		return candidates, nil
	}
	// Even with response_format=json_object, the LLM can still wrap the
	// payload in ```json fences or leading/trailing whitespace — strip
	// before unmarshalling so a noisy response does not silently fall
	// back to the no-op ordering.
	content := stripJSONFence(cr.Choices[0].Message.Content)
	if content == "" {
		return candidates, nil
	}
	var ordered struct {
		Order []int `json:"order"`
	}
	if err := json.Unmarshal([]byte(content), &ordered); err != nil {
		return candidates, nil
	}
	seen := make(map[int]bool, len(ordered.Order))
	reranked := make([]spi.Candidate, 0, len(candidates))
	for _, idx := range ordered.Order {
		i := idx - 1
		if i < 0 || i >= len(candidates) || seen[i] {
			continue
		}
		seen[i] = true
		reranked = append(reranked, candidates[i])
	}
	// Preserve any candidate the LLM forgot to mention so the caller's
	// downstream contract ("all input candidates appear in the output") holds
	// even on partial responses.
	for i := range candidates {
		if !seen[i] {
			seen[i] = true
			reranked = append(reranked, candidates[i])
		}
	}
	return reranked, nil
}

// stripJSONFence removes a leading ```json (or ```) and trailing ```
// that some models wrap around JSON-structured responses even when
// response_format=json_object is set. Keeps everything else intact.
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx > 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
