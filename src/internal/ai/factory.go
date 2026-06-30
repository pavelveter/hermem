// Package ai provides AI client construction (factory) and shared types.
// The factory decouples AI wiring from the config package so non-AI
// domains don't pay for AI client construction in tests.
package ai

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Config holds the parsed AI configuration values needed to construct
// clients. Extracted from config.Config so the ai package has no
// dependency on the config package.
type Config struct {
	// Shared
	Provider string
	URL      string
	Key      string
	Model    string

	// Embedder
	EmbedderProvider string
	EmbedderURL      string
	EmbedderModel    string
	EmbedderKey      string
	EmbedderTimeout  time.Duration

	// Extractor
	ExtractProvider    string
	ExtractURL         string
	ExtractModel       string
	ExtractKey         string
	ExtractTemperature float32
	ExtractTimeout     time.Duration

	// Reranker
	RerankerProvider string
	RerankerURL      string
	RerankerModel    string
	RerankerKey      string
	RerankerTimeout  time.Duration

	// Local embedder
	ModelPath string
}

// Factory constructs AI clients from a typed Config. Non-AI test
// domains can use NewNoopFactory() or nil to skip AI wiring.
type Factory struct {
	cfg Config
}

// NewFactory returns a Factory that constructs clients from cfg.
func NewFactory(cfg Config) *Factory {
	return &Factory{cfg: cfg}
}

// NewEmbedder creates an embedder from the factory config.
func (f *Factory) NewEmbedder() core.Embedder {
	provider := f.cfg.EmbedderProvider
	if provider == "" {
		provider = f.cfg.Provider
	}
	url := f.cfg.EmbedderURL
	if url == "" {
		url = f.cfg.URL
	}
	model := f.cfg.EmbedderModel
	if model == "" {
		model = f.cfg.Model
	}
	key := f.cfg.EmbedderKey
	if key == "" {
		key = f.cfg.Key
	}

	switch provider {
	case "local":
		if f.cfg.ModelPath != "" {
			if _, err := os.Stat(f.cfg.ModelPath); err == nil {
				e, err := NewLocalEmbedder(f.cfg.ModelPath, f.cfg.EmbedderTimeout)
				if err != nil {
					slog.Error("local embedder init failed, falling back to network", "err", err)
				} else {
					slog.Info("using local embedder", "model_path", f.cfg.ModelPath)
					return e
				}
			} else {
				slog.Warn("model_path specified but file not found, falling back to network", "path", f.cfg.ModelPath, "err", err)
			}
		}
		return NewOllamaEmbedder(url, model, f.cfg.EmbedderTimeout)
	case "openai":
		return NewOpenAIEmbedder(url, key, model, f.cfg.EmbedderTimeout)
	default:
		return NewOllamaEmbedder(url, model, f.cfg.EmbedderTimeout)
	}
}

// NewExtractor creates an LLM extractor from the factory config.
func (f *Factory) NewExtractor() core.LLMExtractor {
	provider := f.cfg.ExtractProvider
	if provider == "" {
		provider = f.cfg.Provider
	}
	url := f.cfg.ExtractURL
	if url == "" {
		url = f.cfg.URL
	}
	key := f.cfg.ExtractKey
	if key == "" {
		key = f.cfg.Key
	}

	switch provider {
	case "openai":
		return NewOpenAILLMExtractor(url, key, f.cfg.ExtractModel, f.cfg.ExtractTemperature, f.cfg.ExtractTimeout)
	default:
		return NewOllamaLLMExtractor(url, f.cfg.ExtractModel, f.cfg.ExtractTemperature, f.cfg.ExtractTimeout)
	}
}

// NewReranker creates a reranker from the factory config.
func (f *Factory) NewReranker() core.Reranker {
	provider := f.cfg.RerankerProvider
	if provider == "" {
		return &NoopReranker{}
	}
	url := f.cfg.RerankerURL
	if url == "" {
		url = f.cfg.URL
	}
	model := f.cfg.RerankerModel
	if model == "" {
		model = f.cfg.Model
	}
	key := f.cfg.RerankerKey
	if key == "" {
		key = f.cfg.Key
	}

	switch provider {
	case "openai":
		return NewOpenAIReranker(url, model, key, f.cfg.RerankerTimeout)
	default:
		return NewOllamaReranker(url, model, f.cfg.RerankerTimeout)
	}
}

// NoopEmbedder is a stub embedder for tests that don't need real AI.
type NoopEmbedder struct{}

func (e *NoopEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}
func (e *NoopEmbedder) Ping(_ context.Context) error { return nil }
