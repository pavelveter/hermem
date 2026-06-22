package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

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
	// VectorBackend selects the vector index implementation.
	// "in-memory" (default) — Go brute-force cosine scan, zero-dependency.
	// "sqlite-vec" — indexed KNN via sqlite-vec vec0 virtual table.
	VectorBackend string
	// VectorDim is the embedding dimension used by vec0 virtual table.
	// Only relevant when VectorBackend = "sqlite-vec".
	// Must match the actual output dimension of the configured embedder model.
	VectorDim int
	// APIKey validates X-API-Key on all HTTP endpoints.
	// Empty string disables auth (localhost dev default).
	APIKey string
	// EmbedderTimeout caps each embedder HTTP request.
	EmbedderTimeout time.Duration
	// ExtractTimeout caps each LLM extractor HTTP request.
	ExtractTimeout time.Duration
	// ExtraCategories extends the package-level category allowlist
	// (world, opinion, experience, observation) without recompiling.
	// Operators add domain-specific buckets here, e.g. "schema" for
	// typed schema knowledge. Empty strings are ignored.
	ExtraCategories []string
	// ExtraRelationTypes extends the package-level relation allowlist
	// (prefers, uses, mentions, related_to, part_of, causes, contradicts)
	// without recompiling. Operators add domain-specific edges here,
	// e.g. "supports" or "blocks". Empty strings are ignored.
	ExtraRelationTypes []string
	// Retention controls automatic archival of stale nodes.
	// world facts are permanent; observation nodes past ObservationTTL
	// are flagged archived and excluded from graph walks.
	Retention RetentionPolicy
}

type RetentionPolicy struct {
	ObservationTTL  time.Duration // observations older than this → archived
	RunInterval     time.Duration // how often the GC loop fires
	DeleteBatchSize int           // max nodes archived per cycle (0 = no limit)
}

// LoadConfig parses hermem.ini from `path` exactly as given — no
// resolution to the binary's directory. Production entry points
// (server, CLI main) should call LoadConfigFromBinaryDir instead;
// this lower-level helper is preserved so tests can inject a known
// path without faking os.Executable(). A bare filename like
// "hermem.ini" here is CWD-relative — that's the footgun this
// helper exists to surface, not to fix.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Provider:           "ollama",
		URL:                "http://localhost:11434",
		Model:              "nomic-embed-text",
		DBPath:             "hermem.db",
		ExtractModel:       "qwen2.5-coder:7b",
		ExtractTemperature: 0.1,
		DedupThreshold:     0.88,
		MaxDepthCeiling:    5,
		MaxRetrievedNodes:  100,
		VectorBackend:      "in-memory",
		VectorDim:          768,
		EmbedderTimeout:    30 * time.Second,
		ExtractTimeout:     300 * time.Second,
		Retention: RetentionPolicy{
			ObservationTTL:  90 * 24 * time.Hour,
			RunInterval:     1 * time.Hour,
			DeleteBatchSize: 500,
		},
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	iniFile, err := ini.Load(f)
	if err != nil {
		return nil, fmt.Errorf("parse ini: %w", err)
	}
	iniRef = iniFile

	// sec looks up a section case-insensitively.
	// keyIn returns an *ini.Key matched case-insensitively, or nil.
	keyIn := func(s *ini.Section, name string) *ini.Key {
		if s == nil {
			return nil
		}
		for _, k := range s.Keys() {
			if strings.EqualFold(k.Name(), name) {
				return k
			}
		}
		return nil
	}
	sec := func(name string) *ini.Section {
		name = strings.ToLower(name)
		for _, s := range iniFile.Sections() {
			if strings.ToLower(s.Name()) == name {
				return s
			}
		}
		return nil
	}

	getStr := func(section, key string) (string, bool) {
		k := keyIn(sec(section), key)
		if k == nil {
			return "", false
		}
		return k.String(), true
	}
	getInt := func(section, key string, defaultVal int, minVal int) int {
		k := keyIn(sec(section), key)
		if k == nil {
			return defaultVal
		}
		v, err := k.Int()
		if err != nil || v < minVal {
			return defaultVal
		}
		return v
	}
	getFloat32 := func(section, key string, defaultVal float32) float32 {
		k := keyIn(sec(section), key)
		if k == nil {
			return defaultVal
		}
		v, err := k.Float64()
		if err != nil {
			return defaultVal
		}
		return float32(v)
	}
	getDuration := func(section, key string, defaultVal time.Duration) time.Duration {
		k := keyIn(sec(section), key)
		if k == nil {
			return defaultVal
		}
		v, err := k.Duration()
		if err != nil {
			return defaultVal
		}
		return v
	}

	if v, ok := getStr("embedder", "provider"); ok {
		cfg.Provider = strings.ToLower(v)
	}
	if v, ok := getStr("embedder", "url"); ok {
		cfg.URL = v
	}
	if v, ok := getStr("embedder", "key"); ok {
		cfg.Key = v
	}
	if v, ok := getStr("embedder", "model"); ok {
		cfg.Model = v
	}

	if v, ok := getStr("database", "path"); ok {
		cfg.DBPath = v
	}
	if v, ok := getStr("database", "backend"); ok {
		cfg.VectorBackend = strings.ToLower(v)
	}

	if v, ok := getStr("server", "api_key"); ok {
		cfg.APIKey = v
	}

	cfg.VectorDim = getInt("vector", "dim", cfg.VectorDim, 1)

	if v, ok := getStr("extraction", "provider"); ok {
		cfg.ExtractProvider = strings.ToLower(v)
	}
	if v, ok := getStr("extraction", "url"); ok {
		cfg.ExtractURL = v
	}
	if v, ok := getStr("extraction", "key"); ok {
		cfg.ExtractKey = v
	}
	if v, ok := getStr("extraction", "model"); ok {
		cfg.ExtractModel = v
	}
	cfg.ExtractTemperature = getFloat32("extraction", "temperature", cfg.ExtractTemperature)

	cfg.DedupThreshold = getFloat32("ingestion", "dedup_threshold", cfg.DedupThreshold)

	cfg.MaxDepthCeiling = getInt("retrieval", "depth_ceiling", cfg.MaxDepthCeiling, 0)
	cfg.MaxRetrievedNodes = getInt("retrieval", "max_nodes", cfg.MaxRetrievedNodes, 0)

	cfg.Retention.ObservationTTL = getDuration("retention", "observation_ttl", cfg.Retention.ObservationTTL)
	cfg.Retention.RunInterval = getDuration("retention", "run_interval", cfg.Retention.RunInterval)
	cfg.Retention.DeleteBatchSize = getInt("retention", "batch_size", cfg.Retention.DeleteBatchSize, 0)

	cfg.EmbedderTimeout = getDuration("embedder", "timeout", cfg.EmbedderTimeout)
	cfg.ExtractTimeout = getDuration("extraction", "timeout", cfg.ExtractTimeout)

	cfg.ExtraCategories = parseCSVList(getStrRaw("extraction", "extra_categories"))
	cfg.ExtraRelationTypes = parseCSVList(getStrRaw("extraction", "extra_relation_types"))

	return cfg, nil
}

// parseCSVList splits a comma-separated list of values and trims
// whitespace around each entry. Empty entries (caused by leading /
// trailing /consecutive commas) are dropped so acidental `, , ,`
// from operator typos never surface as blank filter keys. Strings
// already known to the package-level defaults are kept (the caller
// decides whether to dedupe against defaults via Allowed*).
func parseCSVList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// getCSVList reads an INI value as a CSV list. Returns nil for missing
// or empty keys so callers can distinguish "unset" from "explicitly
// empty".
func getCSVList(section, key string) []string {
	return parseCSVList(getStrRaw(section, key))
}

// getStrRaw retrieves a raw string from the INI file with no
// transformation. Companion to the existing getStr closure but lives
// at package scope so the new CSV helpers can call it.
func getStrRaw(section, key string) string {
	k := lookupINIKey(section, key)
	if k == nil {
		return ""
	}
	return k.String()
}

// lookupINIKey returns the *ini.Key for the given section + key pair
// using the same case-insensitive match the rest of the parser uses,
// or nil if not present.
func lookupINIKey(section, key string) *ini.Key {
	// The parser captures an iniFile at LoadConfig time; we keep it
	// accessible via a package-level cell to make this helper work
	// from anywhere without rebuilding the closure plumbing.
	if iniRef == nil {
		return nil
	}
	target := iniRef.Section(section)
	if target == nil {
		return nil
	}
	for _, k := range target.Keys() {
		if strings.EqualFold(k.Name(), key) {
			return k
		}
	}
	return nil
}

// iniRef is set by LoadConfig so helpers like getStrRaw and
// getCSVList can use it after the function returns. It avoids
// re-parsing the file for each access.
var iniRef *ini.File

// AllowedCategories returns the merged category allowlist: package-
// level defaults (world, opinion, experience, observation) plus any
// extras configured via [extraction].extra_categories. Extras do not
// override defaults — both coexist so operators extend without
// retroactively breaking stored data.
//
// The returned map is fresh (allocated here); callers can keep their
// own reference without worrying about concurrent mutation.
func (c *Config) AllowedCategories() map[string]bool {
	out := make(map[string]bool, len(validCategories)+len(c.ExtraCategories))
	for k := range validCategories {
		out[k] = true
	}
	for _, k := range c.ExtraCategories {
		if k == "" {
			continue
		}
		out[k] = true
	}
	return out
}

// AllowedRelationTypes mirrors AllowedCategories for relation types.
// Defaults: prefers, uses, mentions, related_to, part_of, causes,
// contradicts. Extras from [extraction].extra_relation_types append
// to the set without overriding.
func (c *Config) AllowedRelationTypes() map[string]bool {
	out := make(map[string]bool, len(validRelationTypes)+len(c.ExtraRelationTypes))
	for k := range validRelationTypes {
		out[k] = true
	}
	for _, k := range c.ExtraRelationTypes {
		if k == "" {
			continue
		}
		out[k] = true
	}
	return out
}

func (c *Config) NewEmbedder() Embedder {
	switch c.Provider {
	case "openai":
		return NewOpenAIEmbedder(c.URL, c.Key, c.Model, c.EmbedderTimeout)
	default:
		return NewOllamaEmbedder(c.URL, c.Model, c.EmbedderTimeout)
	}
}

func (c *Config) NewExtractor() LLMExtractor {
	provider := orDefault(c.ExtractProvider, c.Provider)
	url := orDefault(c.ExtractURL, c.URL)
	key := orDefault(c.ExtractKey, c.Key)
	cats := c.AllowedCategories()
	rels := c.AllowedRelationTypes()
	switch provider {
	case "openai":
		return NewOpenAILLMExtractor(url, key, c.ExtractModel, c.ExtractTemperature, c.ExtractTimeout, cats, rels)
	default:
		return NewOllamaLLMExtractor(url, c.ExtractModel, c.ExtractTemperature, c.ExtractTimeout, cats, rels)
	}
}

// orDefault returns val if non-empty, otherwise fallback.
func orDefault(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}

// LoadConfigFromBinaryDir is the production entry point: it resolves
// hermem.ini relative to the currently-running executable via
// os.Executable(), so the binary behaves identically regardless of
// the caller's working directory. A `~/.hermes/bin/hermem store`
// invocation lands the same way whether run from its install
// directory, from a cron job's CWD, or from a fresh shell.
//
// A missing ini triggers the same default-config-on-missing policy
// used by LoadConfig (no error, defaults propagated) so an absent
// file is non-fatal — deployments without a hermem.ini still boot
// with the built-in defaults.
func LoadConfigFromBinaryDir() (*Config, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate executable: %w", err)
	}
	return LoadConfigFromDir(filepath.Dir(exePath))
}

// LoadConfigFromDir loads hermem.ini from `dir`. Exported so tests
// can drive the same code path without faking os.Executable() (the
// stdlib doesn't allow that to be replaced at test time).
func LoadConfigFromDir(dir string) (*Config, error) {
	return LoadConfig(filepath.Join(dir, "hermem.ini"))
}

// Validate checks config invariants that would cause silent misbehaviour
// at runtime. Returns nil on success.
func (c *Config) Validate() error {
	if c.DedupThreshold < 0 || c.DedupThreshold > 1 {
		return fmt.Errorf("dedup_threshold must be in [0, 1], got %.2f", c.DedupThreshold)
	}
	if c.VectorDim <= 0 {
		return fmt.Errorf("vector.dim must be positive, got %d", c.VectorDim)
	}
	if c.Provider != "ollama" && c.Provider != "openai" {
		return fmt.Errorf("embedder.provider must be 'ollama' or 'openai', got %q", c.Provider)
	}
	if c.URL == "" {
		return fmt.Errorf("embedder.url must not be empty")
	}
	return nil
}

// resolveDBPath interprets cfg.DBPath in a hermem-binary-aware way:
// absolute paths are returned unchanged so operators can pin the DB
// to /var/lib/hermem/ or similar; relative paths are joined to the
// binary's directory so the DB is colocated with the binary, not
// the caller's working directory.
//
// Note: os.Executable reports the kernel-resolved path, not the
// symlink path. A binary installed via a symlink (e.g.
// /usr/local/bin/hermem -> /opt/hermem-real/hermem) reads its
// ini and writes its DB in /opt/hermem-real/, not /usr/local/bin/.
// We deliberately do NOT follow the symlink: it matches Go stdlib
// semantics and avoids platform-specific os.Readlink logic. If an
// operator needs symlink-following, that's a future PR.
//
// On os.Executable failure the original path is returned unchanged
// so InitDB surfaces the original failure mode rather than masking
// it behind a binary-resolution error.
func resolveDBPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	exePath, err := os.Executable()
	if err != nil {
		return p
	}
	return filepath.Join(filepath.Dir(exePath), p)
}
