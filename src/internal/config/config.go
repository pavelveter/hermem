// Package config parses hermem.ini and provides configuration validation.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/ini.v1"

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

// LoadConfig parses hermem.ini from path. A missing file returns defaults (no error).
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
		Retention: core.RetentionPolicy{
			ObservationTTL:  90 * 24 * time.Hour,
			RunInterval:     1 * time.Hour,
			DeleteBatchSize: 500,
		},
		Ranking: core.RankingWeight{
			VectorWeight:          0.7,
			RecencyWeight:         0.3,
			DepthPenalty:          0.05,
			RecencyHalfLifeHours:  720,
			TemporalWeight:        0.1,
			TemporalHalfLifeHours: 720,
			CentralityWeight:      0.05,
		},
		RerankerTimeout: 30 * time.Second,
		Schema:          DefaultSchemaConfig(false),
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

	sec := func(name string) *ini.Section {
		name = strings.ToLower(name)
		for _, s := range iniFile.Sections() {
			if strings.ToLower(s.Name()) == name {
				return s
			}
		}
		return nil
	}
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
	getList := func(section, key string) []string {
		k := keyIn(sec(section), key)
		if k == nil {
			return nil
		}
		return ParseCSVList(k.String())
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

	cfg.Ranking.VectorWeight = getFloat32("ranking", "vector_weight", cfg.Ranking.VectorWeight)
	cfg.Ranking.RecencyWeight = getFloat32("ranking", "recency_weight", cfg.Ranking.RecencyWeight)
	cfg.Ranking.DepthPenalty = getFloat32("ranking", "depth_penalty", cfg.Ranking.DepthPenalty)
	cfg.Ranking.RecencyHalfLifeHours = getFloat32("ranking", "recency_half_life_hours", cfg.Ranking.RecencyHalfLifeHours)
	cfg.Ranking.TemporalWeight = getFloat32("ranking", "temporal_weight", cfg.Ranking.TemporalWeight)
	cfg.Ranking.TemporalHalfLifeHours = getFloat32("ranking", "temporal_half_life_hours", cfg.Ranking.TemporalHalfLifeHours)
	cfg.Ranking.CentralityWeight = getFloat32("ranking", "centrality_weight", cfg.Ranking.CentralityWeight)

	if v, ok := getStr("reranker", "provider"); ok {
		cfg.RerankerProvider = strings.ToLower(v)
	}
	if v, ok := getStr("reranker", "url"); ok {
		cfg.RerankerURL = v
	}
	if v, ok := getStr("reranker", "model"); ok {
		cfg.RerankerModel = v
	}
	if v, ok := getStr("reranker", "key"); ok {
		cfg.RerankerKey = v
	}
	cfg.RerankerTimeout = getDuration("reranker", "timeout", cfg.RerankerTimeout)

	cfg.ExtraCategories = getList("extraction", "extra_categories")
	cfg.ExtraRelationTypes = getList("extraction", "extra_relation_types")
	if schemaSection := sec("schema"); schemaSection != nil {
		schema, err := ParseSchemaSection(schemaSection, path)
		if err != nil {
			return nil, err
		}
		cfg.Schema = schema
	}

	return cfg, nil
}

// Validate checks config invariants.
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
	if err := ValidateSchema(c.Schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	return nil
}

// ValidateCategory returns an error if the category is not allowed.
func (c *Config) ValidateCategory(category string) error {
	if !c.AllowedCategories()[category] {
		return fmt.Errorf("unknown category: %s", category)
	}
	return nil
}

// ValidateRelation returns an error if the relation type is not allowed.
func (c *Config) ValidateRelation(relation string) error {
	if !c.AllowedRelationTypes()[relation] {
		return fmt.Errorf("unknown relation_type: %s", relation)
	}
	return nil
}

// ValidateState checks that the status is valid for the given stateful category.
func (c *Config) ValidateState(category, status string) error {
	if !c.Schema.StatefulCategories[category] {
		return nil
	}
	if !c.Schema.ValidStates[status] {
		return fmt.Errorf("invalid status %q for category %q", status, category)
	}
	return nil
}

// AllowedCategories returns the merged category allowlist.
func (c *Config) AllowedCategories() map[string]bool {
	schema := c.Schema
	if schema.AllowedCategories == nil {
		schema = DefaultSchemaConfig(false)
	}
	out := make(map[string]bool, len(schema.AllowedCategories)+len(c.ExtraCategories))
	for k := range schema.AllowedCategories {
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

// AllowedRelationTypes returns the merged relation allowlist.
func (c *Config) AllowedRelationTypes() map[string]bool {
	schema := c.Schema
	if schema.AllowedRelations == nil {
		schema = DefaultSchemaConfig(false)
	}
	out := make(map[string]bool, len(schema.AllowedRelations)+len(c.ExtraRelationTypes))
	for k := range schema.AllowedRelations {
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

// DefaultSchemaConfig returns a SchemaConfig with built-in defaults.
// Delegates to core.DefaultSchemaConfig for the canonical definition.
func DefaultSchemaConfig(stateful bool) core.SchemaConfig {
	return core.DefaultSchemaConfig(stateful)
}

// ParseSchemaSection parses the [schema] section of hermem.ini.
func ParseSchemaSection(section *ini.Section, path string) (core.SchemaConfig, error) {
	allowedKeys := map[string]bool{
		"allowed_categories":  true,
		"allowed_relations":   true,
		"stateful_categories": true,
		"valid_states":        true,
		"relation_blocking":   true,
		"state_unblocking":    true,
		"relation_recovery":   true,
	}
	for _, k := range section.Keys() {
		name := strings.ToLower(k.Name())
		if name == "name" {
			continue
		}
		if !allowedKeys[name] {
			return core.SchemaConfig{}, fmt.Errorf("%s:%d: unknown [schema] key %q", path, FindConfigLine(path, k.Name()), k.Name())
		}
	}
	schema := DefaultSchemaConfig(true)
	if v := ParseCSVList(section.Key("allowed_categories").String()); len(v) > 0 {
		schema.AllowedCategories = BoolMap(v)
	} else {
		return core.SchemaConfig{}, fmt.Errorf("%s:%d: [schema].allowed_categories must not be empty", path, FindConfigLine(path, "allowed_categories"))
	}
	if v := ParseCSVList(section.Key("allowed_relations").String()); len(v) > 0 {
		schema.AllowedRelations = BoolMap(v)
	} else {
		return core.SchemaConfig{}, fmt.Errorf("%s:%d: [schema].allowed_relations must not be empty", path, FindConfigLine(path, "allowed_relations"))
	}
	stateful := ParseCSVList(section.Key("stateful_categories").String())
	schema.StatefulCategories = BoolMap(stateful)
	states := ParseCSVList(section.Key("valid_states").String())
	schema.ValidStateOrder = states
	schema.ValidStates = BoolMap(states)
	if len(stateful) > 0 && len(states) == 0 {
		return core.SchemaConfig{}, fmt.Errorf("%s:%d: [schema].valid_states required when stateful_categories is set", path, FindConfigLine(path, "valid_states"))
	}
	for category := range schema.StatefulCategories {
		if !schema.AllowedCategories[category] {
			return core.SchemaConfig{}, fmt.Errorf("%s:%d: stateful category %q is not in allowed_categories", path, FindConfigLine(path, "stateful_categories"), category)
		}
	}
	if v := strings.TrimSpace(section.Key("relation_blocking").String()); v != "" {
		schema.RelationBlocking = v
	}
	if v := strings.TrimSpace(section.Key("state_unblocking").String()); v != "" {
		schema.StateUnblocking = v
	}
	if v := strings.TrimSpace(section.Key("relation_recovery").String()); v != "" {
		schema.RelationRecovery = v
	}
	for _, rel := range []string{schema.RelationBlocking, schema.RelationRecovery} {
		if rel != "" && !schema.AllowedRelations[rel] {
			return core.SchemaConfig{}, fmt.Errorf("%s:%d: schema relation %q is not in allowed_relations", path, FindConfigLine(path, rel), rel)
		}
	}
	if schema.StateUnblocking != "" && len(schema.ValidStates) > 0 && !schema.ValidStates[schema.StateUnblocking] {
		return core.SchemaConfig{}, fmt.Errorf("%s:%d: state_unblocking %q is not in valid_states", path, FindConfigLine(path, "state_unblocking"), schema.StateUnblocking)
	}
	return schema, nil
}

// BoolMap converts a string slice to a set.
func BoolMap(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			out[value] = true
		}
	}
	return out
}

// FindConfigLine returns the 1-based line number of a key in a config file.
func FindConfigLine(path, key string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	needle := strings.ToLower(key)
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), needle) {
			return i + 1
		}
	}
	return 0
}

// ParseCSVList splits a comma-separated list, trimming whitespace and dropping empties.
func ParseCSVList(s string) []string {
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

// ValidateSchema checks a SchemaConfig for internal consistency.
func ValidateSchema(s core.SchemaConfig) error {
	seen := make(map[string]bool, len(s.ValidStateOrder))
	for _, state := range s.ValidStateOrder {
		if seen[state] {
			return fmt.Errorf("duplicate state %q in valid_states", state)
		}
		seen[state] = true
	}
	if len(s.StatefulCategories) > 0 && len(s.ValidStateOrder) == 0 {
		return fmt.Errorf("stateful_categories set but valid_states is empty")
	}
	if s.StateUnblocking != "" && len(s.ValidStates) > 0 && !s.ValidStates[s.StateUnblocking] {
		return fmt.Errorf("state_unblocking %q is not in valid_states", s.StateUnblocking)
	}
	for _, rel := range []string{s.RelationBlocking, s.RelationRecovery} {
		if rel != "" && len(s.AllowedRelations) > 0 && !s.AllowedRelations[rel] {
			return fmt.Errorf("schema relation %q is not in allowed_relations", rel)
		}
	}
	return nil
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

// ResolveDBPath interprets a DB path relative to the binary.
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

// SortedKeys returns the sorted keys of a bool map.
func SortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FirstStatefulCategory returns the alphabetically first stateful category.
func FirstStatefulCategory(schema core.SchemaConfig) string {
	keys := SortedKeys(schema.StatefulCategories)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

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
