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

// OllamaLLMExtractor implements core.LLMExtractor against the Ollama /api/chat endpoint.
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
	// Retry logic — exponential backoff on 5xx and network errors, fail fast on 4xx
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
		var result core.ExtractionResult
		if err := json.Unmarshal([]byte(cr.Message.Content), &result); err != nil {
			lastErr = fmt.Errorf("parse extraction result: %w", err)
			continue
		}
		return &result, nil
	}
	return nil, fmt.Errorf("ollama extract: retries exhausted: %w", lastErr)
}

// OpenAILLMExtractor implements core.LLMExtractor against the OpenAI /v1/chat/completions endpoint.
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

// buildExtractionPrompt is shared between Ollama and OpenAI extractors.
func buildExtractionPrompt(dialog string) string {
	return fmt.Sprintf(`Extract entities and relations from this dialog. Output JSON: {"entities":[{"id":"...","category":"...","content":"...","relations":[{"target_id":"...","relation_type":"..."}]}]}

Dialog:
%s`, dialog)
}
