package main

import (
	"bufio"
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Provider     string
	URL          string
	Key          string
	Model        string
	DBPath       string
	ExtractModel string
	// DedupThreshold is the cosine-similarity floor above which an
	// incoming entity is considered a duplicate of an existing one and
	// is merged rather than inserted. Cosine similarity lives in [0, 1]
	// (unit-length dot product); 0.88 means "very close in vector space"
	// and is empirically a good default for short factual text.
	// Lower for noisier inputs; raise to merge only near-duplicates.
	DedupThreshold float32
	// MaxDepthCeiling is the hard upper bound on requested graph-walk
	// depth. Calls asking for a larger depth get silently clamped so a
	// pathological request cannot blow up the server. 0 disables the cap.
	MaxDepthCeiling int
	// MaxRetrievedNodes is a soft cap on the total nodes returned by a
	// single RetrieveContext call, protecting response size and memory
	// against dense graph walks. 0 disables the cap.
	MaxRetrievedNodes int
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Provider:          "ollama",
		URL:               "http://localhost:11434",
		Model:             "nomic-embed-text",
		DBPath:            "hermem.db",
		ExtractModel:      "qwen2.5-coder:7b",
		DedupThreshold:    0.88,
		MaxDepthCeiling:   5,
		MaxRetrievedNodes: 100,
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
		case "ingestion.dedup_threshold":
			if v, err := strconv.ParseFloat(val, 32); err == nil {
				cfg.DedupThreshold = float32(v)
			} else {
				log.Printf("config: invalid ingestion.dedup_threshold %q, keeping default %.2f: %v", val, cfg.DedupThreshold, err)
			}
		case "retrieval.depth_ceiling":
			if v, err := strconv.Atoi(val); err == nil && v >= 0 {
				cfg.MaxDepthCeiling = v
			} else {
				log.Printf("config: invalid retrieval.depth_ceiling %q, keeping default %d: %v", val, cfg.MaxDepthCeiling, err)
			}
		case "retrieval.max_nodes":
			if v, err := strconv.Atoi(val); err == nil && v >= 0 {
				cfg.MaxRetrievedNodes = v
			} else {
				log.Printf("config: invalid retrieval.max_nodes %q, keeping default %d: %v", val, cfg.MaxRetrievedNodes, err)
			}
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
