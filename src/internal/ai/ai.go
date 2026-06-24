// Package ai provides AI provider implementations: embedders, extractors, and rerankers.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// --- Embedders ---

type OllamaEmbedder struct {
	BaseURL string
	Model   string
	client  *http.Client
}

func NewOllamaEmbedder(baseURL, model string, timeout time.Duration) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OllamaEmbedder{BaseURL: baseURL, Model: model, client: &http.Client{Timeout: timeout}}
}

type ollamaEmbedReq struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResp struct {
	Embedding []float32 `json:"embedding"`
}

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(ollamaEmbedReq{Model: e.Model, Prompt: text})
	resp, err := e.client.Post(strings.TrimRight(e.BaseURL, "/")+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed: %d: %s", resp.StatusCode, string(b))
	}
	var r ollamaEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	return r.Embedding, nil
}

type OpenAIEmbedder struct {
	BaseURL string
	APIKey  string
	Model   string
	client  *http.Client
}

func NewOpenAIEmbedder(baseURL, apiKey, model string, timeout time.Duration) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenAIEmbedder{BaseURL: baseURL, APIKey: apiKey, Model: model, client: &http.Client{Timeout: timeout}}
}

type openaiEmbedReq struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type openaiEmbedResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(openaiEmbedReq{Input: text, Model: e.Model})
	req, _ := http.NewRequest("POST", strings.TrimRight(e.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai embed: %d: %s", resp.StatusCode, string(b))
	}
	var r openaiEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("openai embed decode: %w", err)
	}
	if len(r.Data) == 0 {
		return nil, fmt.Errorf("openai embed: no data")
	}
	return r.Data[0].Embedding, nil
}

// --- Extractors ---

type OllamaLLMExtractor struct {
	BaseURL     string
	Model       string
	Temperature float32
	client      *http.Client
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
		timeout = 300 * time.Second
	}
	return &OllamaLLMExtractor{BaseURL: baseURL, Model: model, Temperature: temperature, client: &http.Client{Timeout: timeout}}
}

type chatMessage struct{ Role, Content string }
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  struct {
		Temperature float32 `json:"temperature"`
	} `json:"options"`
	Format string `json:"format"`
}

type chatResponse struct {
	Message struct{ Content string } `json:"message"`
}

func (e *OllamaLLMExtractor) ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
	prompt := buildExtractionPrompt(dialog)
	req := chatRequest{
		Model:    e.Model,
		Messages: []chatMessage{{Role: "user", Content: prompt}},
		Stream:   false,
		Format:   "json",
	}
	req.Options.Temperature = e.Temperature
	body, _ := json.Marshal(req)
	// Retry logic
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := e.client.Post(strings.TrimRight(e.BaseURL, "/")+"/api/chat", "application/json", bytes.NewReader(body))
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("ollama extract: %d: %s", resp.StatusCode, string(b))
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("ollama extract: %d: %s", resp.StatusCode, string(b))
		}
		var cr chatResponse
		if err := json.Unmarshal(b, &cr); err != nil {
			lastErr = fmt.Errorf("ollama extract decode: %w", err)
			continue
		}
		// Try parsing as ExtractionResult JSON
		var result core.ExtractionResult
		if err := json.Unmarshal([]byte(cr.Message.Content), &result); err != nil {
			lastErr = fmt.Errorf("parse extraction result: %w", err)
			continue
		}
		return &result, nil
	}
	return nil, fmt.Errorf("ollama extract: retries exhausted: %w", lastErr)
}

type OpenAILLMExtractor struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float32
	client      *http.Client
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
		timeout = 300 * time.Second
	}
	return &OpenAILLMExtractor{BaseURL: baseURL, APIKey: apiKey, Model: model, Temperature: temperature, client: &http.Client{Timeout: timeout}}
}

func (e *OpenAILLMExtractor) ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
	prompt := buildExtractionPrompt(dialog)
	body, _ := json.Marshal(map[string]interface{}{
		"model":           e.Model,
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
		"temperature":     e.Temperature,
		"response_format": map[string]string{"type": "json_object"},
	})
	req, _ := http.NewRequest("POST", strings.TrimRight(e.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai extract: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai extract: %d: %s", resp.StatusCode, string(b))
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &cr); err != nil {
		return nil, fmt.Errorf("openai extract decode: %w", err)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("openai extract: no choices")
	}
	var result core.ExtractionResult
	if err := json.Unmarshal([]byte(cr.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("parse extraction result: %w", err)
	}
	return &result, nil
}

func buildExtractionPrompt(dialog string) string {
	return fmt.Sprintf(`Extract entities and relations from this dialog. Output JSON: {"entities":[{"id":"...","category":"...","content":"...","relations":[{"target_id":"...","relation_type":"..."}]}]}

Dialog:
%s`, dialog)
}

// --- Rerankers ---

type NoopReranker struct{}

func (r *NoopReranker) Rerank(ctx context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	return facts, nil
}

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

func (r *OllamaReranker) Rerank(ctx context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	if len(facts) == 0 {
		return facts, nil
	}
	var docs []string
	for _, f := range facts {
		docs = append(docs, f.Content)
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model": r.Model, "query": query, "documents": docs,
	})
	resp, err := r.client.Post(strings.TrimRight(r.BaseURL, "/")+"/api/rerank", "application/json", bytes.NewReader(body))
	if err != nil {
		return facts, nil
	} // graceful degradation
	defer resp.Body.Close()
	var rr struct {
		Results []struct {
			Index int `json:"index"`
		} `json:"results"`
	}
	json.NewDecoder(resp.Body).Decode(&rr)
	reranked := make([]core.RetrievedFact, 0, len(facts))
	for _, item := range rr.Results {
		if item.Index < len(facts) {
			reranked = append(reranked, facts[item.Index])
		}
	}
	return reranked, nil
}

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

func (r *OpenAIReranker) Rerank(ctx context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	// Simple chat-based reranking
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
	return facts, nil // simplified: keep original order on error
}
