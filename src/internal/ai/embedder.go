package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaEmbedder implements core.Embedder against the Ollama /api/embeddings endpoint.
type OllamaEmbedder struct {
	BaseURL   string
	Model     string
	client    *http.Client
	resilient *ResilientClient
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
	c := &http.Client{Timeout: timeout}
	return &OllamaEmbedder{
		BaseURL:   baseURL,
		Model:     model,
		client:    c,
		resilient: NewResilientClient(c, 4, DefaultBackoffs), // 1 initial + 3 retries
	}
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
	url := strings.TrimRight(e.BaseURL, "/") + "/api/embeddings"
	// NewRequestWithContext pins ctx onto the in-flight request so an
	// upstream cancellation aborts the connection even if the response
	// body is mid-read.
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	captured := body
	req.Body = io.NopCloser(strings.NewReader(string(captured)))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(captured))), nil
	}
	resp, err := e.resilient.Do(ctx, req)
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

// OpenAIEmbedder implements core.Embedder against the OpenAI /v1/embeddings endpoint.
type OpenAIEmbedder struct {
	BaseURL   string
	APIKey    string
	Model     string
	client    *http.Client
	resilient *ResilientClient
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
	c := &http.Client{Timeout: timeout}
	return &OpenAIEmbedder{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     model,
		client:    c,
		resilient: NewResilientClient(c, 4, DefaultBackoffs),
	}
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
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(e.BaseURL, "/")+"/embeddings", nil)
	if err != nil {
		return nil, fmt.Errorf("openai embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)
	captured := body
	req.Body = io.NopCloser(strings.NewReader(string(captured)))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(captured))), nil
	}
	resp, err := e.resilient.Do(ctx, req)
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
