package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// OllamaLLMExtractor implements core.LLMExtractor against the Ollama /api/chat endpoint.
//
// The Ollama chat response nests the extraction JSON inside `cr.Message.Content`
// (i.e. the LLM returns a string that is itself JSON). doPOST streams the
// *outer* envelope into a chatResponse; the inner json.Unmarshal on the
// extracted Content string is preserved in the method body below.
type OllamaLLMExtractor struct {
	BaseURL     string
	Model       string
	Temperature float32
	http        *httpClient
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
	return &OllamaLLMExtractor{
		BaseURL:     baseURL,
		Model:       model,
		Temperature: temperature,
		http:        newHTTPClient(baseURL, "", timeout, RetryPolicy{MaxAttempts: 4}),
	}
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
	var cr chatResponse
	if err := e.http.doPOST(ctx, "/api/chat", req, &cr); err != nil {
		return nil, fmt.Errorf("ollama extract: %w", err)
	}
	var result core.ExtractionResult
	if err := json.Unmarshal([]byte(cr.Message.Content), &result); err != nil {
		return nil, fmt.Errorf("parse extraction result: %w", err)
	}
	return &result, nil
}

// OpenAILLMExtractor implements core.LLMExtractor against the OpenAI /v1/chat/completions endpoint.
//
// Same double-decode pattern as Ollama: doPOST decodes the outer chat envelope
// into a local struct, then json.Unmarshal on cr.Choices[0].Message.Content
// produces *core.ExtractionResult.
type OpenAILLMExtractor struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float32
	http        *httpClient
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
	return &OpenAILLMExtractor{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       model,
		Temperature: temperature,
		http:        newHTTPClient(baseURL, apiKey, timeout, RetryPolicy{MaxAttempts: 4}),
	}
}

func (e *OpenAILLMExtractor) ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
	prompt := buildExtractionPrompt(dialog)
	body := map[string]interface{}{
		"model":           e.Model,
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
		"temperature":     e.Temperature,
		"response_format": map[string]string{"type": "json_object"},
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := e.http.doPOST(ctx, "/chat/completions", body, &cr); err != nil {
		return nil, fmt.Errorf("openai extract: %w", err)
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
