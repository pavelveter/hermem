package main

import (
	"bufio"
	"os"
	"strings"
)

type Config struct {
	Provider     string
	URL          string
	Key          string
	Model        string
	DBPath       string
	ExtractModel string
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Provider:     "ollama",
		URL:          "http://localhost:11434",
		Model:        "nomic-embed-text",
		DBPath:       "hermem.db",
		ExtractModel: "qwen2.5-coder:7b",
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	section := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}

		if line[0] == '[' && line[len(line)-1] == ']' {
			section = line[1 : len(line)-1]
			continue
		}

		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])

		switch strings.ToLower(section + "." + key) {
		case "embedder.provider":
			cfg.Provider = strings.ToLower(val)
		case "embedder.url":
			cfg.URL = val
		case "embedder.key":
			cfg.Key = val
		case "embedder.model":
			cfg.Model = val
		case "database.path":
			cfg.DBPath = val
		case "extraction.model":
			cfg.ExtractModel = val
		}
	}

	return cfg, scanner.Err()
}

func (c *Config) NewEmbedder() Embedder {
	switch c.Provider {
	case "openai":
		return NewOpenAIEmbedder(c.URL, c.Key, c.Model)
	default:
		return NewOllamaEmbedder(c.URL, c.Model)
	}
}

func (c *Config) NewExtractor() LLMExtractor {
	return NewOllamaLLMExtractor(c.URL, c.ExtractModel)
}
