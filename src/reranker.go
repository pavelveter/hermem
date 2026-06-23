package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

// Reranker re-orders or scores a set of retrieved facts based on their
// relevance to the user's query. After graph expansion completes and
// facts are scored by the CompositeScorer, a Reranker gets a final
// chance to reorder the bucket before it's returned to the caller.
//
// A nil Reranker (or NoopReranker) means "skip reranking" — the
// CompositeScorer ordering is the final output.
type Reranker interface {
	Rerank(ctx context.Context, query string, facts []RetrievedFact) ([]RetrievedFact, error)
}

// NoopReranker returns facts unchanged. Use when no reranker is
// configured.
type NoopReranker struct{}

func (r *NoopReranker) Rerank(_ context.Context, _ string, facts []RetrievedFact) ([]RetrievedFact, error) {
	return facts, nil
}

// OllamaReranker calls Ollama's /api/rerank endpoint which hosts
// cross-encoder models like mxbai-rerank-base. Each fact's content
// is sent as a document; the endpoint returns relevance scores.
type OllamaReranker struct {
	BaseURL string
	Model   string
	client  *http.Client
}

// ollamaRerankRequest mirrors the Ollama /api/rerank payload.
type ollamaRerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

// ollamaRerankResponse mirrors the Ollama /api/rerank response.
type ollamaRerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

func NewOllamaReranker(baseURL, model string, timeout time.Duration) *OllamaReranker {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "mxbai-rerank-base"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OllamaReranker{
		BaseURL: baseURL,
		Model:   model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (r *OllamaReranker) Rerank(ctx context.Context, query string, facts []RetrievedFact) ([]RetrievedFact, error) {
	if len(facts) == 0 {
		return facts, nil
	}

	docs := make([]string, len(facts))
	for i, f := range facts {
		docs[i] = f.Content
	}

	body := ollamaRerankRequest{
		Model:     r.Model,
		Query:     query,
		Documents: docs,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.BaseURL+"/api/rerank", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	slog.Debug("ollama rerank call",
		"model", r.Model,
		"doc_count", len(docs),
	)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank: %s", resp.Status)
	}

	var result ollamaRerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}

	// Copy original facts and apply cross-encoder relevance scores.
	// Facts not scored by the API keep their original RankingScore.
	out := make([]RetrievedFact, len(facts))
	copy(out, facts)
	for _, r := range result.Results {
		if r.Index >= 0 && r.Index < len(facts) {
			out[r.Index].RankingScore = float32(r.RelevanceScore)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].RankingScore > out[j].RankingScore
	})

	return out, nil
}

// OpenAIReranker uses a chat-completion LLM to re-rank facts by sending
// the query + numbered facts and asking the model to return them in
// relevance order. Works with any OpenAI-compatible endpoint.
type OpenAIReranker struct {
	BaseURL string
	Model   string
	Key     string
	client  *http.Client
}

// openaiRerankRequest mirrors the OpenAI chat completions payload.
type openaiRerankRequest struct {
	Model       string                `json:"model"`
	Messages    []openaiRerankMessage `json:"messages"`
	MaxTokens   int                   `json:"max_tokens"`
	Temperature float64               `json:"temperature"`
}

type openaiRerankMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiRerankResponse mirrors the OpenAI chat completions response.
type openaiRerankResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func NewOpenAIReranker(baseURL, model, key string, timeout time.Duration) *OpenAIReranker {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenAIReranker{
		BaseURL: baseURL,
		Model:   model,
		Key:     key,
		client:  &http.Client{Timeout: timeout},
	}
}

func (r *OpenAIReranker) Rerank(ctx context.Context, query string, facts []RetrievedFact) ([]RetrievedFact, error) {
	if len(facts) <= 1 {
		return facts, nil
	}

	// Build a prompt with numbered facts and ask the model to sort by relevance.
	var factsList string
	for i, f := range facts {
		factsList += fmt.Sprintf("%d. %s\n", i+1, f.Content)
	}

	prompt := fmt.Sprintf(
		`You are a relevance ranker. Given a query and a list of facts, return the facts ordered by relevance (most relevant first). Output ONLY the ordered numbers separated by commas, like: 3,1,5,2,4

Query: %s

Facts:
%s

Ranked order (numbers only):`,
		query, factsList,
	)

	body := openaiRerankRequest{
		Model: r.Model,
		Messages: []openaiRerankMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   128,
		Temperature: 0.0,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal llm rerank request: %w", err)
	}

	endpoint := r.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create llm rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.Key != "" {
		req.Header.Set("Authorization", "Bearer "+r.Key)
	}

	slog.Debug("openai rerank call",
		"model", r.Model,
		"fact_count", len(facts),
	)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai rerank request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai rerank: %s", resp.Status)
	}

	var result openaiRerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode llm rerank response: %w", err)
	}
	if len(result.Choices) == 0 {
		return facts, nil
	}

	content := result.Choices[0].Message.Content
	// Parse comma-separated numbers from the response.
	indices := parseRankingResponse(content, len(facts))

	n := len(facts)
	out := make([]RetrievedFact, 0, n)
	used := make(map[int]bool)
	for _, idx := range indices {
		if idx >= 1 && idx <= n && !used[idx] {
			used[idx] = true
			f := facts[idx-1]
			f.RankingScore = 1.0 - float32(len(out))*0.01
			out = append(out, f)
		}
	}
	// Append any facts the model missed, with lower scores
	for i := range facts {
		if !used[i+1] {
			f := facts[i]
			f.RankingScore = 0.4 - float32(len(out)-len(used))*0.005
			out = append(out, f)
		}
	}

	return out, nil
}

// parseRankingResponse extracts ordered numbers from an LLM response.
// Handles formats like "3,1,5,2,4" or "3 1 5 2 4" or "3, 1, 5, 2, 4".
func parseRankingResponse(content string, maxN int) []int {
	var nums []int
	n := 0
	for i := 0; i < len(content); i++ {
		c := content[i]
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			if n > 0 && n <= maxN+100 { // allow some slack
				nums = append(nums, n)
			}
			n = 0
		}
	}
	if n > 0 && n <= maxN+100 {
		nums = append(nums, n)
	}
	return nums
}

// NewReranker returns a Reranker based on config, or nil if no
// reranker is configured. Follows the same provider convention as
// embedder and extractor: "ollama" → cross-encoder /api/rerank,
// "openai" → chat-completion /chat/completions. Empty → nil.
func (c *Config) NewReranker() Reranker {
	if c.RerankerProvider == "" {
		return nil
	}
	switch c.RerankerProvider {
	case "openai":
		return NewOpenAIReranker(c.RerankerURL, c.RerankerModel, c.RerankerKey, c.RerankerTimeout)
	default:
		return NewOllamaReranker(c.RerankerURL, c.RerankerModel, c.RerankerTimeout)
	}
}
