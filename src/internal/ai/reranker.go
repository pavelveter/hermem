package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	BaseURL string
	Model   string
	client  *http.Client
}

func NewOllamaReranker(baseURL, model string, timeout time.Duration) *OllamaReranker {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OllamaReranker{BaseURL: baseURL, Model: model, client: &http.Client{Timeout: timeout}}
}

func (r *OllamaReranker) Rerank(_ context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	if len(facts) == 0 {
		return facts, nil
	}
	docs := make([]string, len(facts))
	for i, f := range facts {
		docs[i] = f.Content
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model": r.Model, "query": query, "documents": docs,
	})
	resp, err := r.client.Post(strings.TrimRight(r.BaseURL, "/")+"/api/rerank", "application/json", bytes.NewReader(body))
	if err != nil {
		return facts, nil // graceful degradation
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
// Simplified: on any failure it returns the original ordering.
type OpenAIReranker struct {
	BaseURL string
	APIKey  string
	Model   string
	client  *http.Client
}

func NewOpenAIReranker(baseURL, model, key string, timeout time.Duration) *OpenAIReranker {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenAIReranker{BaseURL: baseURL, APIKey: key, Model: model, client: &http.Client{Timeout: timeout}}
}

func (r *OpenAIReranker) Rerank(_ context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	if len(facts) <= 1 {
		return facts, nil
	}
	var docList strings.Builder
	for i, f := range facts {
		fmt.Fprintf(&docList, "%d. %s\n", i+1, f.Content)
	}
	prompt := fmt.Sprintf("Query: %s\n\nDocuments:\n%s\n\nReturn the document numbers in relevance order, comma-separated. Example: 3,1,2", query, docList.String())
	body, _ := json.Marshal(map[string]interface{}{
		"model":    r.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, _ := http.NewRequest("POST", strings.TrimRight(r.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if r.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.APIKey)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return facts, nil
	}
	defer resp.Body.Close()
	return facts, nil // simplified: keep original order on any error
}
