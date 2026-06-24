package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// OllamaEmbedder implements core.Embedder against the Ollama /api/embeddings endpoint.
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
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := e.client.Post(strings.TrimRight(e.BaseURL, "/")+"/api/embeddings", "application/json", bytes.NewReader(body))
		if err != nil {
			lastErr = fmt.Errorf("ollama embed: %w", err)
			time.Sleep(time.Duration(attempt+1)*time.Second + time.Duration(rand.Intn(500))*time.Millisecond)
			continue
		}
		if resp.StatusCode == 503 || resp.StatusCode == 429 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("ollama embed: %d: %s", resp.StatusCode, string(b))
			time.Sleep(time.Duration(attempt+1)*time.Second + time.Duration(rand.Intn(500))*time.Millisecond)
			continue
		}
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("ollama embed: %d: %s", resp.StatusCode, string(b))
		}
		var r ollamaEmbedResp
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("ollama embed decode: %w", err)
			time.Sleep(time.Duration(attempt+1)*time.Second + time.Duration(rand.Intn(500))*time.Millisecond)
			continue
		}
		resp.Body.Close()
		return r.Embedding, nil
	}
	return nil, fmt.Errorf("ollama embed: retries exhausted: %w", lastErr)
}

// OpenAIEmbedder implements core.Embedder against the OpenAI /v1/embeddings endpoint.
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
	req, err := newRequestWithRetry("POST", strings.TrimRight(e.BaseURL, "/")+"/embeddings", body)
	if err != nil {
		return nil, fmt.Errorf("openai embed: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	resp, err := retryDo(e.client, req, 3)
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
