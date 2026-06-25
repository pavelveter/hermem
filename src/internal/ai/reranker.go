package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
type OllamaReranker struct {
	BaseURL   string
	Model     string
	client    *http.Client
	resilient *ResilientClient
}

func NewOllamaReranker(baseURL, model string, timeout time.Duration) *OllamaReranker {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	c := &http.Client{Timeout: timeout}
	return &OllamaReranker{
		BaseURL:   baseURL,
		Model:     model,
		client:    c,
		resilient: NewResilientClient(c, 3, DefaultBackoffs()), // 1 + 2 retries; graceful-degrade after
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
	url := strings.TrimRight(r.BaseURL, "/") + "/api/rerank"
	body, _ := json.Marshal(map[string]interface{}{
		"model": r.Model, "query": query, "documents": docs,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return facts, nil // graceful degradation
	}
	req.Header.Set("Content-Type", "application/json")
	captured := body
	req.Body = io.NopCloser(strings.NewReader(string(captured)))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(captured))), nil
	}
	// ResilientClient.Do retries on 5xx/429/network; on final failure
	// we degrade to the input ordering rather than surface an error.
	resp, err := r.resilient.Do(ctx, req)
	if err != nil {
		return facts, nil
	}
	defer resp.Body.Close()
	var rr struct {
		Results []struct {
			Index int `json:"index"`
		} `json:"results"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&rr)
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
	BaseURL   string
	APIKey    string
	Model     string
	client    *http.Client
	resilient *ResilientClient
}

func NewOpenAIReranker(baseURL, model, key string, timeout time.Duration) *OpenAIReranker {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	c := &http.Client{Timeout: timeout}
	return &OpenAIReranker{
		BaseURL:   baseURL,
		APIKey:    key,
		Model:     model,
		client:    c,
		resilient: NewResilientClient(c, 3, DefaultBackoffs()),
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
	body, _ := json.Marshal(map[string]interface{}{
		"model":           r.Model,
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(r.BaseURL, "/")+"/chat/completions", nil)
	if err != nil {
		return facts, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if r.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.APIKey)
	}
	captured := body
	req.Body = io.NopCloser(strings.NewReader(string(captured)))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(captured))), nil
	}
	resp, err := r.resilient.Do(ctx, req)
	if err != nil {
		return facts, nil
	}
	defer resp.Body.Close()
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return facts, nil
	}
	if len(cr.Choices) == 0 {
		return facts, nil
	}
	// Even with response_format=json_object, the LLM can still wrap the
	// payload in ```json fences or leading/trailing whitespace — strip
	// before unmarshalling so a noisy response does not silently fall
	// back to the no-op ordering.
	content := strings.TrimSpace(cr.Choices[0].Message.Content)
	content = stripJSONFence(content)
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
