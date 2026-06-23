package main

import (
	"fmt"
	"log/slog"
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
	// Ranking weights for the composite scorer. Populated from [ranking]
	// config section. Zero values mean "use defaults" (0.7 / 0.3 / 0.05).
	Ranking RankingWeight
	// Reranker config for post-retrieval reordering. When RerankerProvider
	// is empty, no reranker is used. Follows the same provider convention
	// as embedder/extractor: "ollama" (cross-encoder /api/rerank),
	// "openai" (chat-completion ranking).
	RerankerProvider string
	RerankerURL      string
	RerankerModel    string
	RerankerKey      string
	RerankerTimeout  time.Duration
	Schema           SchemaConfig
}

type SchemaConfig struct {
	AllowedCategories  map[string]bool
	AllowedRelations   map[string]bool
	StatefulCategories map[string]bool
	ValidStates        map[string]bool
	ValidStateOrder    []string
	RelationBlocking   string
	StateUnblocking    string
	RelationRecovery   string
	StatefulEnabled    bool
}

type RetentionPolicy struct {
	ObservationTTL  time.Duration // observations older than this → archived
	RunInterval     time.Duration // how often the GC loop fires
	DeleteBatchSize int           // max nodes archived per cycle (0 = no limit)
}

// RankingWeight holds the three tunable parameters for the composite
// ranker. Populated from the [ranking] config section. Any zero value
// falls back to the default at point-of-use.
type RankingWeight struct {
	VectorWeight          float32
	RecencyWeight         float32
	DepthPenalty          float32
	RecencyHalfLifeHours  float32
	TemporalWeight        float32
	TemporalHalfLifeHours float32
	CentralityWeight      float32
}

// LoadConfig parses hermem.ini from `path` exactly as given — no
// resolution to the binary's directory. Production entry points
// (server, CLI main) should call LoadConfigFromBinaryDir instead;
// this lower-level helper is preserved so tests can inject a known
// path without faking os.Executable(). A bare filename like
// "hermem.ini" here is CWD-relative — that's the footgun this
// helper exists to surface, not to fix.
//
// Sprint 1 change: parser state is no longer leaked to a package-level
// `iniRef` cell. Every get* helper here is a closure over the local
// `iniFile` returned by `ini.Load`. After LoadConfig returns, no
// external reader can mutate the parsed tree, so config becomes
// effectively immutable once constructed. The csv helpers
// (parseCSVList, etc.) live at package scope because they have no
// ini dependency — they're pure string utilities.
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
		Ranking: RankingWeight{
			VectorWeight:          0.7,
			RecencyWeight:         0.3,
			DepthPenalty:          0.05,
			RecencyHalfLifeHours:  720,
			TemporalWeight:        0.1,
			TemporalHalfLifeHours: 720,
			CentralityWeight:      0.05,
		},
		RerankerTimeout: 30 * time.Second,
		Schema:          defaultSchemaConfig(false),
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

	// sec looks up a section case-insensitively.
	sec := func(name string) *ini.Section {
		name = strings.ToLower(name)
		for _, s := range iniFile.Sections() {
			if strings.ToLower(s.Name()) == name {
				return s
			}
		}
		return nil
	}
	// keyIn matches a key case-insensitively in a section.
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
		return parseCSVList(k.String())
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

	// [ranking] section — tunable composite scorer weights
	cfg.Ranking.VectorWeight = getFloat32("ranking", "vector_weight", cfg.Ranking.VectorWeight)
	cfg.Ranking.RecencyWeight = getFloat32("ranking", "recency_weight", cfg.Ranking.RecencyWeight)
	cfg.Ranking.DepthPenalty = getFloat32("ranking", "depth_penalty", cfg.Ranking.DepthPenalty)
	cfg.Ranking.RecencyHalfLifeHours = getFloat32("ranking", "recency_half_life_hours", cfg.Ranking.RecencyHalfLifeHours)
	cfg.Ranking.TemporalWeight = getFloat32("ranking", "temporal_weight", cfg.Ranking.TemporalWeight)
	cfg.Ranking.TemporalHalfLifeHours = getFloat32("ranking", "temporal_half_life_hours", cfg.Ranking.TemporalHalfLifeHours)
	cfg.Ranking.CentralityWeight = getFloat32("ranking", "centrality_weight", cfg.Ranking.CentralityWeight)

	// [reranker] section — optional post-retrieval reranker
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
		schema, err := parseSchemaSection(schemaSection, path)
		if err != nil {
			return nil, err
		}
		cfg.Schema = schema
	}

	return cfg, nil
}

func defaultSchemaConfig(stateful bool) SchemaConfig {
	cats := map[string]bool{
		"world":       true,
		"opinion":     true,
		"experience":  true,
		"observation": true,
	}
	rels := map[string]bool{}
	for k := range validRelationTypes {
		rels[k] = true
	}
	return SchemaConfig{
		AllowedCategories:  cats,
		AllowedRelations:   rels,
		StatefulCategories: map[string]bool{},
		ValidStates:        map[string]bool{},
		ValidStateOrder:    nil,
		RelationBlocking:   "blocked_by",
		StateUnblocking:    "completed",
		RelationRecovery:   "recovers_via",
		StatefulEnabled:    stateful,
	}
}

func parseSchemaSection(section *ini.Section, path string) (SchemaConfig, error) {
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
			return SchemaConfig{}, fmt.Errorf("%s:%d: unknown [schema] key %q", path, findConfigLine(path, k.Name()), k.Name())
		}
	}
	schema := defaultSchemaConfig(true)
	if v := parseCSVList(section.Key("allowed_categories").String()); len(v) > 0 {
		schema.AllowedCategories = boolMap(v)
	} else {
		return SchemaConfig{}, fmt.Errorf("%s:%d: [schema].allowed_categories must not be empty", path, findConfigLine(path, "allowed_categories"))
	}
	if v := parseCSVList(section.Key("allowed_relations").String()); len(v) > 0 {
		schema.AllowedRelations = boolMap(v)
	} else {
		return SchemaConfig{}, fmt.Errorf("%s:%d: [schema].allowed_relations must not be empty", path, findConfigLine(path, "allowed_relations"))
	}
	stateful := parseCSVList(section.Key("stateful_categories").String())
	schema.StatefulCategories = boolMap(stateful)
	states := parseCSVList(section.Key("valid_states").String())
	schema.ValidStateOrder = states
	schema.ValidStates = boolMap(states)
	if len(stateful) > 0 && len(states) == 0 {
		return SchemaConfig{}, fmt.Errorf("%s:%d: [schema].valid_states required when stateful_categories is set", path, findConfigLine(path, "valid_states"))
	}
	for category := range schema.StatefulCategories {
		if !schema.AllowedCategories[category] {
			return SchemaConfig{}, fmt.Errorf("%s:%d: stateful category %q is not in allowed_categories", path, findConfigLine(path, "stateful_categories"), category)
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
			return SchemaConfig{}, fmt.Errorf("%s:%d: schema relation %q is not in allowed_relations", path, findConfigLine(path, rel), rel)
		}
	}
	if schema.StateUnblocking != "" && len(schema.ValidStates) > 0 && !schema.ValidStates[schema.StateUnblocking] {
		return SchemaConfig{}, fmt.Errorf("%s:%d: state_unblocking %q is not in valid_states", path, findConfigLine(path, "state_unblocking"), schema.StateUnblocking)
	}
	return schema, nil
}

func boolMap(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func findConfigLine(path, key string) int {
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

// sprint 1 note: getCSVList/getStrRaw helpers deleted — replaced by
// closure-based getList. parseCSVList (the pure string utility) stays
// at package scope so any future cell can reference it without
// re-parsing INI state.

// AllowedCategories returns the merged category allowlist: package-
// level defaults (world, opinion, experience, observation) plus any
// extras configured via [extraction].extra_categories. Extras do not
// override defaults — both coexist so operators extend without
// retroactively breaking stored data.
//
// The returned map is fresh (allocated here); callers can keep their
// own reference without worrying about concurrent mutation.
func (c *Config) AllowedCategories() map[string]bool {
	schema := c.Schema
	if schema.AllowedCategories == nil {
		schema = defaultSchemaConfig(false)
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

// AllowedRelationTypes mirrors AllowedCategories for relation types.
// Defaults: prefers, uses, mentions, related_to, part_of, causes,
// contradicts. Extras from [extraction].extra_relation_types append
// to the set without overriding.
func (c *Config) AllowedRelationTypes() map[string]bool {
	schema := c.Schema
	if schema.AllowedRelations == nil {
		schema = defaultSchemaConfig(false)
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

func (c *Config) ValidateCategory(category string) error {
	if !c.AllowedCategories()[category] {
		return fmt.Errorf("unknown category: %s", category)
	}
	return nil
}

func (c *Config) ValidateRelation(relation string) error {
	if !c.AllowedRelationTypes()[relation] {
		return fmt.Errorf("unknown relation_type: %s", relation)
	}
	return nil
}

func (c *Config) ValidateState(category, status string) error {
	if !c.Schema.StatefulCategories[category] {
		return nil
	}
	if !c.Schema.ValidStates[status] {
		return fmt.Errorf("invalid status %q for category %q", status, category)
	}
	return nil
}

func (c *Config) NewEmbedder() Embedder {
	switch c.Provider {
	case "openai":
		return NewOpenAIEmbedder(c.URL, c.Key, c.Model, c.EmbedderTimeout)
	default:
		return NewOllamaEmbedder(c.URL, c.Model, c.EmbedderTimeout)
	}
}

// Fingerprint returns a deterministic hash of the schema config.
// Delegates to the package-level HashSchema for use outside InitDB
// (e.g. in config reload paths that need to compare fingerprints).
func (s SchemaConfig) Fingerprint() string {
	return HashSchema(s)
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
// at runtime. Returns nil on success. Also validates the embedded schema
// via ValidateSchema.
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

// ValidateSchema checks a SchemaConfig for internal consistency before
// the server starts. Detects duplicate states, missing initial state,
// and unreachable states in the FSM. Called at startup and on SIGHUP.
// Returns nil if the schema is valid.
func ValidateSchema(s SchemaConfig) error {
	// Duplicate state detection in the ordered slice.
	seen := make(map[string]bool, len(s.ValidStateOrder))
	for _, state := range s.ValidStateOrder {
		if seen[state] {
			return fmt.Errorf("duplicate state %q in valid_states", state)
		}
		seen[state] = true
	}

	// If stateful categories exist, there must be at least one valid
	// state (the initial state).
	if len(s.StatefulCategories) > 0 && len(s.ValidStateOrder) == 0 {
		return fmt.Errorf("stateful_categories set but valid_states is empty")
	}

	// state_unblocking must appear in valid_states if stateful FSM is active.
	if s.StateUnblocking != "" && len(s.ValidStates) > 0 && !s.ValidStates[s.StateUnblocking] {
		return fmt.Errorf("state_unblocking %q is not in valid_states", s.StateUnblocking)
	}

	// Blocking and recovery relations must be in allowed_relations.
	for _, rel := range []string{s.RelationBlocking, s.RelationRecovery} {
		if rel != "" && len(s.AllowedRelations) > 0 && !s.AllowedRelations[rel] {
			return fmt.Errorf("schema relation %q is not in allowed_relations", rel)
		}
	}

	return nil
}

// resolveDBPath interprets cfg.DBPath in a hermem-binary-aware way:
// absolute paths are returned unchanged so operators can pin the DB
// to /var/lib/hermem/ or similar; relative paths are joined to the
// binary's directory so the DB is colocated with the binary, not
// the caller's working directory.
//
// Symlink observability (#1 review): os.Executable reports the
// path the kernel sees, not the path the operator typed. A binary
// installed via a symlink (e.g. /usr/local/bin/hermem ->
// /opt/hermem-real/hermem) returns /opt/hermem-real from
// os.Executable already, so the join has historically been
// correct. We add an explicit EvalSymlinks pass to (a) keep the
// behaviour consistent if a future Go release changes how
// os.Executable reports paths and (b) emit a debug event when
// raw vs. resolved directories diverge so operators debugging
// "where is my DB file" can see the symlink chain at slog.Debug
// without re-running with strace.
//
// Behaviour preserved: the DB still lands in the same directory
// os.Executable pointed to before this change. EvalSymlinks just
// gives us the auditability hook without changing the on-disk
// layout for the common no-symlink case.
//
// Failure policy:
//   - os.Executable failure → return `p` unchanged.
//   - EvalSymlinks failure (broken symlink, target missing,
//     permission denied) → fall back to the kernel-resolved
//     directory rather than propagating the error, matching the
//     pre-PR resolveDBPath contract that InitDB surfaces real
//     errors instead of masking them behind resolution noise.
//   - EvalSymlinks equal to the raw dir → no debug event emitted
//     (common path: 99% of deployments); only the divergence
//     case produces a log entry, so debug streams stay quiet.
//   - EvalSymlinks non-nil error → emits db_path_symlink_eval_failed
//     with the raw dir and the underlying error (stringified) so
//     debug-mode operators can see WHY fallback to rawDir was
//     chosen (broken symlink, permission denied, ENOENT). Note
//     this case still returns the join against rawDir — the same
//     fallback as os.Executable failure, just with an additional
//     audit hook.
func resolveDBPath(p string) string {
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
		slog.Debug("db_path_symlink_eval_failed",
			"raw", rawDir,
			"error", evalErr.Error(),
			"db_path", filepath.Join(rawDir, p),
		)
		return filepath.Join(rawDir, p)
	}
	if resolvedDir != rawDir {
		slog.Debug("db_path_symlink_resolved",
			"raw", rawDir,
			"resolved", resolvedDir,
			"db_path", filepath.Join(resolvedDir, p),
		)
	}
	return filepath.Join(resolvedDir, p)
}
