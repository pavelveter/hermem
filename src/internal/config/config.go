// Package config parses hermem.ini and provides configuration validation.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pavelveter/hermem/src/internal/auth"

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
	ModelPath          string
	ExtractProvider    string
	ExtractURL         string
	ExtractKey         string
	ExtractModel       string
	ExtractTemperature float32
	DedupThreshold     float32
	MaxDepthCeiling    int
	MaxRetrievedNodes  int
	TokenBudget        int // soft token limit for retrieval; 0 = unlimited
	VectorBackend      string
	VectorDim          int
	APIKey             string
	APIKeys            []auth.Key
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
	// AutoMigrate gates InitDB's auto-apply-migrations path. When false
	// (the production default after §4 closure), InitDB refuses to boot
	// if the DB has pending migrations or integrity mismatches and
	// points the operator at `./hermem db migrate apply` (or opt-in
	// via set `auto_migrate = true` in hermem.ini for dev / docker).
	// §4 audit closure — see docs/CHANGELOG.md.
	AutoMigrate bool
}

// NewEmbedder creates an embedder from config.
// Delegates to ai.Factory for construction.
func (c *Config) NewEmbedder() core.Embedder {
	return c.aiFactory().NewEmbedder()
}

// NewExtractor creates an LLM extractor from config.
// Delegates to ai.Factory for construction.
func (c *Config) NewExtractor() core.LLMExtractor {
	return c.aiFactory().NewExtractor()
}

// NewReranker creates a reranker from config.
// Delegates to ai.Factory for construction.
func (c *Config) NewReranker() core.Reranker {
	return c.aiFactory().NewReranker()
}

func (c *Config) aiFactory() *ai.Factory {
	return ai.NewFactory(ai.Config{
		Provider: c.Provider,
		URL:      c.URL,
		Key:      c.Key,
		Model:    c.Model,

		ExtractProvider:    c.ExtractProvider,
		ExtractURL:         c.ExtractURL,
		ExtractModel:       c.ExtractModel,
		ExtractKey:         c.ExtractKey,
		ExtractTemperature: c.ExtractTemperature,
		ExtractTimeout:     c.ExtractTimeout,

		RerankerProvider: c.RerankerProvider,
		RerankerURL:      c.RerankerURL,
		RerankerModel:    c.RerankerModel,
		RerankerKey:      c.RerankerKey,
		RerankerTimeout:  c.RerankerTimeout,

		EmbedderTimeout: c.EmbedderTimeout,
		ModelPath:       c.ModelPath,
	})
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

func DefaultConfigPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return "hermem.ini"
	}
	return filepath.Join(filepath.Dir(exePath), "hermem.ini")
}

// LoadConfigFromBinaryDir resolves hermem.ini with the following precedence:
// 1. HERMEM_INI environment variable (if set)
// 2. hermem.ini next to the running binary
func LoadConfigFromBinaryDir() (*Config, error) {
	if envPath := os.Getenv("HERMEM_INI"); envPath != "" {
		return LoadConfig(envPath)
	}
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
