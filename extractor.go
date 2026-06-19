package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OllamaLLMExtractor struct {
	BaseURL string
	Model   string
}

func NewOllamaLLMExtractor(baseURL, model string) *OllamaLLMExtractor {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "qwen2.5-coder:7b"
	}
	return &OllamaLLMExtractor{BaseURL: baseURL, Model: model}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Format   string        `json:"format,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Done    bool         `json:"done"`
}

func (e *OllamaLLMExtractor) ExtractEntities(dialog string) (*ExtractionResult, error) {
	systemPrompt := `You are a knowledge extraction assistant. Extract entities and relations from dialog text.

Categories:
- world: facts, definitions, objective knowledge
- opinion: preferences, beliefs, subjective views
- experience: past events, interactions, what happened
- observation: patterns noticed, anomalies, insights

Rules:
1. Extract atomic, self-contained entities
2. Each entity needs a unique kebab-case id
3. Relations connect entities with descriptive types like "prefers", "uses", "mentions", "related_to"
4. Only include clear, useful knowledge
5. Return ONLY valid JSON matching this schema:
{"entities":[{"id":"string","category":"world|opinion|experience|observation","content":"string","relations":[{"target_id":"string","relation_type":"string"}]}]}

Dialog:` 

	req := chatRequest{
		Model: e.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: dialog},
		},
		Stream: false,
		Format: "json",
		Options: map[string]any{
			"temperature": 0.1,
		},
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal chat request: %w", err)
	}

	resp, err := http.Post(e.BaseURL+"/api/chat", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to call Ollama chat API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read chat response: %w", err)
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal chat response: %w", err)
	}

	content := strings.TrimSpace(chatResp.Message.Content)
	if content == "" {
		return &ExtractionResult{Entities: []ExtractedEntity{}}, nil
	}

	var result ExtractionResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// Fallback: if LLM returned non-JSON, create a single world entity
		return &ExtractionResult{
			Entities: []ExtractedEntity{
				{
					ID:       fmt.Sprintf("entity-%d", hashString(dialog)%100000),
					Category: "world",
					Content:  strings.TrimSpace(dialog),
				},
			},
		}, nil
	}

	if result.Entities == nil {
		result.Entities = []ExtractedEntity{}
	}

	return &result, nil
}

func hashString(s string) int {
	h := 0
	for _, c := range s {
		h = 31*h + int(c)
	}
	if h < 0 {
		return -h
	}
	return h
}
