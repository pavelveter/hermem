package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// NoopReranker returns the input facts unchanged — used when no reranker is configured.
type NoopReranker struct{}

func (r *NoopReranker) Rerank(_ context.Context, _ string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	return facts, nil
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
		http:    newHTTPClient(baseURL, "", timeout, 3), // 1 + 2 retries; graceful-degrade after
	}
}

func (r *OllamaReranker) Rerank(ctx context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	if len(facts) == 0 {
		return facts, nil
	}
	docs := make([]string, len(facts))
	for i, f := range facts {
		docs[i] = f.Content
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
		return facts, nil // graceful degradation
	}
	reranked := make([]core.RetrievedFact, 0, len(facts))
	for _, item := range rr.Results {
		if item.Index >= 0 && item.Index < len(facts) {
			reranked = append(reranked, facts[item.Index])
		}
	}
	if len(reranked) == 0 {
		return facts, nil
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
		http:    newHTTPClient(baseURL, key, timeout, 3),
	}
}

func (r *OpenAIReranker) Rerank(ctx context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	if len(facts) <= 1 {
		return facts, nil
	}
	var docList strings.Builder
	for i, f := range facts {
		fmt.Fprintf(&docList, "%d. %s\n", i+1, f.Content)
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
		return facts, nil
	}
	if len(cr.Choices) == 0 {
		return facts, nil
	}
	// Even with response_format=json_object, the LLM can still wrap the
	// payload in ```json fences or leading/trailing whitespace — strip
	// before unmarshalling so a noisy response does not silently fall
	// back to the no-op ordering.
	content := stripJSONFence(cr.Choices[0].Message.Content)
	if content == "" {
		return facts, nil
	}
	var ordered struct {
		Order []int `json:"order"`
	}
	if err := json.Unmarshal([]byte(content), &ordered); err != nil {
		return facts, nil
	}
	seen := make(map[int]bool, len(ordered.Order))
	reranked := make([]core.RetrievedFact, 0, len(facts))
	for _, idx := range ordered.Order {
		i := idx - 1
		if i < 0 || i >= len(facts) || seen[i] {
			continue
		}
		seen[i] = true
		reranked = append(reranked, facts[i])
	}
	// Preserve any fact the LLM forgot to mention so the caller's
	// downstream contract ("all input facts appear in the output") holds
	// even on partial responses.
	for i := range facts {
		if !seen[i] {
			seen[i] = true
			reranked = append(reranked, facts[i])
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
