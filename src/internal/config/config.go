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
	// RateLimitEnabled gates the HTTP rate-limit middleware. When
	// false (the default) no limiter is constructed and no per-
	// request overhead is incurred. Opt-in for production; hermem
	// developers running locally can leave it off.
	RateLimitEnabled bool
	// RateLimitRPS is the token-bucket refill rate (tokens/sec).
	// Default 10. Must be > 0. Typed as float32 to match the
	// existing rank-weight / dedup-precision convention in this
	// struct; 7 significant digits is plenty for "10.0 rps".
	RateLimitRPS float32
	// RateLimitBurst is the per-key bucket capacity. Default
	// ceil(RPS) when unset. Must be >= 1.
	RateLimitBurst int
	// RateLimitKeyBy selects the keying strategy: "ip" (per
	// RemoteAddr), "api_key" (per X-API-Key, falls back to IP),
	// or "global" (one bucket for the whole server). Default
	// "ip". Case-insensitive.
	RateLimitKeyBy string
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

// LoadConfigFromSources resolves hermem.ini with the following precedence:
//  1. flagPath — string passed to --config (non-empty wins regardless of env)
//  2. $HERMEM_INI — environment variable
//  3. hermem.ini alongside the running binary (os.Executable())
//
// Precedence is enforced strictly: flag > env > binary-dir. An operator
// that sets both --config and HERMEM_INI expects the explicit flag to
// win; the env var is treated as a deployment-level default that
// individual invocations can override.
//
// flagPath semantics: a non-empty string short-circuits to branch 1
// regardless of whether other branches would also succeed. An empty
// string (the stdlib flag default when --config is omitted) is treated
// as unset and falls through to branches 2 and 3. No whitespace
// trimming: `--config " "` passes " " through to LoadConfig, which
// then falls back to defaults because the path is non-existent. This
// matches stdlib flag semantics — operators wanting whitespace to mean
// "unset" should pass an empty string.
//
// Each branch logs at INFO with the source kind so an operator can audit
// which file the running binary is consuming (the slog message includes
// the path so a config-typo becomes obvious in production logs).
func LoadConfigFromSources(flagPath string) (*Config, error) {
	if flagPath != "" {
		slog.Info("config: loading from --config flag", "path", flagPath)
		return LoadConfig(flagPath)
	}
	if envPath := os.Getenv("HERMEM_INI"); envPath != "" {
		slog.Info("config: loading from HERMEM_INI env", "path", envPath)
		return LoadConfig(envPath)
	}
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate executable: %w", err)
	}
	dir := filepath.Dir(exePath)
	slog.Info("config: loading from binary directory", "dir", dir)
	return LoadConfigFromDir(dir)
}

// LoadConfigFromBinaryDir resolves hermem.ini with the following precedence:
// 1. HERMEM_INI environment variable (if set)
// 2. hermem.ini next to the running binary
//
// Deprecated: callers should use LoadConfigFromSources instead. New code
// should pass an explicit flag value (or "") to LoadConfigFromSources.
// This shim is kept stable because at least one integration test asserts
// the env-var-and-fallback contract explicitly and removing it would
// orphan that assertion.
func LoadConfigFromBinaryDir() (*Config, error) {
	return LoadConfigFromSources("")
}

// LoadConfigFromDir loads hermem.ini from dir.
func LoadConfigFromDir(dir string) (*Config, error) {
	return LoadConfig(filepath.Join(dir, "hermem.ini"))
}
