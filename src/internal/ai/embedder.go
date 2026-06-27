package ai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Compile-time interface assertions.
var (
	_ core.Embedder = (*OllamaEmbedder)(nil)
	_ core.Embedder = (*OpenAIEmbedder)(nil)
)

// OllamaEmbedder implements core.Embedder against the Ollama /api/embeddings endpoint.
type OllamaEmbedder struct {
	BaseURL string
	Model   string
	http    *httpClient
}

func NewOllamaEmbedder(baseURL, model string, timeout time.Duration) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaEmbedder{
		BaseURL: baseURL,
		Model:   model,
		http:    newHTTPClient(baseURL, "", timeout, 4), // 1 initial + 3 retries
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
	var r ollamaEmbedResp
	if err := e.http.doPOST(ctx, "/api/embeddings", ollamaEmbedReq{Model: e.Model, Prompt: text}, &r); err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	return r.Embedding, nil
}

// Ping checks whether the Ollama server is reachable.
func (e *OllamaEmbedder) Ping(ctx context.Context) error {
	resp, err := e.http.doGET(ctx, "/api/tags")
	if err != nil {
		return fmt.Errorf("ollama ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama ping: status %d", resp.StatusCode)
	}
	return nil
}

// OpenAIEmbedder implements core.Embedder against the OpenAI /v1/embeddings endpoint.
type OpenAIEmbedder struct {
	BaseURL string
	APIKey  string
	Model   string
	http    *httpClient
}

func NewOpenAIEmbedder(baseURL, apiKey, model string, timeout time.Duration) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenAIEmbedder{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		http:    newHTTPClient(baseURL, apiKey, timeout, 4),
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
	var r openaiEmbedResp
	if err := e.http.doPOST(ctx, "/embeddings", openaiEmbedReq{Input: text, Model: e.Model}, &r); err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	if len(r.Data) == 0 {
		return nil, fmt.Errorf("openai embed: no data")
	}
	return r.Data[0].Embedding, nil
}

// Ping checks whether the OpenAI API is reachable.
func (e *OpenAIEmbedder) Ping(ctx context.Context) error {
	resp, err := e.http.doGET(ctx, "/models")
	if err != nil {
		return fmt.Errorf("openai ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai ping: status %d", resp.StatusCode)
	}
	return nil
}
