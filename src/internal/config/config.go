// Package config parses hermem.ini and provides configuration validation.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pavelveter/hermem/src/internal/ai"
	"github.com/pavelveter/hermem/src/internal/core"
)

// Config holds all runtime configuration parsed from hermem.ini.
type Config struct {
	Provider           string
	URL                string
	Key                string
	Model              string
	DBPath             string
	ExtractProvider    string
	ExtractURL         string
	ExtractKey         string
	ExtractModel       string
	ExtractTemperature float32
	DedupThreshold     float32
	MaxDepthCeiling    int
	MaxRetrievedNodes  int
	VectorBackend      string
	VectorDim          int
	APIKey             string
	EmbedderTimeout    time.Duration
	ExtractTimeout     time.Duration
	ExtraCategories    []string
	ExtraRelationTypes []string
	Retention          core.RetentionPolicy
	Ranking            core.RankingWeight
	RerankerProvider   string
	RerankerURL        string
	RerankerModel      string
	RerankerKey        string
	RerankerTimeout    time.Duration
	Schema             core.SchemaConfig
}

// orDefault returns val if non-empty, else fallback.
func orDefault(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}

// NewEmbedder creates an embedder from config.
func (c *Config) NewEmbedder() core.Embedder {
	switch c.Provider {
	case "openai":
		return ai.NewOpenAIEmbedder(c.URL, c.Key, c.Model, c.EmbedderTimeout)
	default:
		return ai.NewOllamaEmbedder(c.URL, c.Model, c.EmbedderTimeout)
	}
}

// NewExtractor creates an LLM extractor from config.
func (c *Config) NewExtractor() core.LLMExtractor {
	provider := orDefault(c.ExtractProvider, c.Provider)
	url := orDefault(c.ExtractURL, c.URL)
	key := orDefault(c.ExtractKey, c.Key)
	switch provider {
	case "openai":
		return ai.NewOpenAILLMExtractor(url, key, c.ExtractModel, c.ExtractTemperature, c.ExtractTimeout)
	default:
		return ai.NewOllamaLLMExtractor(url, c.ExtractModel, c.ExtractTemperature, c.ExtractTimeout)
	}
}

// NewReranker creates a reranker from config.
func (c *Config) NewReranker() core.Reranker {
	if c.RerankerProvider == "" {
		return &ai.NoopReranker{}
	}
	url := orDefault(c.RerankerURL, c.URL)
	model := orDefault(c.RerankerModel, c.Model)
	key := orDefault(c.RerankerKey, c.Key)
	switch c.RerankerProvider {
	case "openai":
		return ai.NewOpenAIReranker(url, model, key, c.RerankerTimeout)
	default:
		return ai.NewOllamaReranker(url, model, c.RerankerTimeout)
	}
}

// ResolveDBPath interprets a DB path relative to the binary, following symlinks.
func ResolveDBPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	exePath, err := os.Executable()
	if err != nil {
		return p
	}
	rawDir := filepath.Dir(exePath)
	resolvedDir, evalErr := filepath.EvalSymlinks(rawDir)
	if evalErr != nil {
		slog.Debug("db_path_symlink_eval_failed", "raw", rawDir, "error", evalErr.Error(), "db_path", filepath.Join(rawDir, p))
		return filepath.Join(rawDir, p)
	}
	if resolvedDir != rawDir {
		slog.Debug("db_path_symlink_resolved", "raw", rawDir, "resolved", resolvedDir, "db_path", filepath.Join(resolvedDir, p))
	}
	return filepath.Join(resolvedDir, p)
}

// LoadConfigFromBinaryDir resolves hermem.ini relative to the running binary.
func LoadConfigFromBinaryDir() (*Config, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate executable: %w", err)
	}
	return LoadConfigFromDir(filepath.Dir(exePath))
}

// LoadConfigFromDir loads hermem.ini from dir.
func LoadConfigFromDir(dir string) (*Config, error) {
	return LoadConfig(filepath.Join(dir, "hermem.ini"))
}
