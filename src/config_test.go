package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigDefaultsWhenFileMissing(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := LoadConfig(filepath.Join(tmp, "does-not-exist.ini"))
	if err != nil {
		t.Fatalf("LoadConfig returned error for missing file: %v", err)
	}

	want := struct {
		Provider           string
		URL                string
		Model              string
		DBPath             string
		ExtractModel       string
		ExtractTemperature float32
		DedupThreshold     float32
		MaxDepthCeiling    int
		MaxRetrievedNodes  int
		VectorBackend      string
		VectorDim          int
	}{
		"ollama", "http://localhost:11434", "nomic-embed-text",
		"hermem.db", "qwen2.5-coder:7b", 0.1, 0.88, 5, 100,
		"in-memory", 768,
	}

	if cfg.Provider != want.Provider {
		t.Errorf("Provider = %q, want %q", cfg.Provider, want.Provider)
	}
	if cfg.URL != want.URL {
		t.Errorf("URL = %q, want %q", cfg.URL, want.URL)
	}
	if cfg.Model != want.Model {
		t.Errorf("Model = %q, want %q", cfg.Model, want.Model)
	}
	if cfg.DBPath != want.DBPath {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, want.DBPath)
	}
	if cfg.ExtractModel != want.ExtractModel {
		t.Errorf("ExtractModel = %q, want %q", cfg.ExtractModel, want.ExtractModel)
	}
	if cfg.ExtractTemperature != want.ExtractTemperature {
		t.Errorf("ExtractTemperature = %v, want %v", cfg.ExtractTemperature, want.ExtractTemperature)
	}
	if cfg.DedupThreshold != want.DedupThreshold {
		t.Errorf("DedupThreshold = %v, want %v", cfg.DedupThreshold, want.DedupThreshold)
	}
	if cfg.MaxDepthCeiling != want.MaxDepthCeiling {
		t.Errorf("MaxDepthCeiling = %d, want %d", cfg.MaxDepthCeiling, want.MaxDepthCeiling)
	}
	if cfg.MaxRetrievedNodes != want.MaxRetrievedNodes {
		t.Errorf("MaxRetrievedNodes = %d, want %d", cfg.MaxRetrievedNodes, want.MaxRetrievedNodes)
	}
	if cfg.VectorBackend != want.VectorBackend {
		t.Errorf("VectorBackend = %q, want %q", cfg.VectorBackend, want.VectorBackend)
	}
	if cfg.VectorDim != want.VectorDim {
		t.Errorf("VectorDim = %d, want %d", cfg.VectorDim, want.VectorDim)
	}
	// ExtractProvider/URL/Key should be empty (zero value) by default
	if cfg.ExtractProvider != "" {
		t.Errorf("ExtractProvider = %q, want empty", cfg.ExtractProvider)
	}
	if cfg.ExtractURL != "" {
		t.Errorf("ExtractURL = %q, want empty", cfg.ExtractURL)
	}
	if cfg.ExtractKey != "" {
		t.Errorf("ExtractKey = %q, want empty", cfg.ExtractKey)
	}
}

func TestLoadConfigParsesAllKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermem.ini")
	contents := `# full hermem config
[embedder]
provider = openai
url       = https://api.example.com/v1
key       = sk-test
model     = text-embedding-3-small

[database]
path = /tmp/hermem-test.db

	[extraction]
	model = gpt-4o-mini
	temperature = 0.05

	[ingestion]
dedup_threshold = 0.95

	[retrieval]
	depth_ceiling = 7
	max_nodes     = 25

	[vector]
	dim = 1536
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", cfg.Provider)
	}
	if cfg.URL != "https://api.example.com/v1" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.Key != "sk-test" {
		t.Errorf("Key = %q", cfg.Key)
	}
	if cfg.Model != "text-embedding-3-small" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.DBPath != "/tmp/hermem-test.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.ExtractModel != "gpt-4o-mini" {
		t.Errorf("ExtractModel = %q, want gpt-4o-mini", cfg.ExtractModel)
	}
	if cfg.ExtractTemperature != 0.05 {
		t.Errorf("ExtractTemperature = %v, want 0.05", cfg.ExtractTemperature)
	}
	if cfg.DedupThreshold != 0.95 {
		t.Errorf("DedupThreshold = %v, want 0.95", cfg.DedupThreshold)
	}
	if cfg.MaxDepthCeiling != 7 {
		t.Errorf("MaxDepthCeiling = %d, want 7", cfg.MaxDepthCeiling)
	}
	if cfg.MaxRetrievedNodes != 25 {
		t.Errorf("MaxRetrievedNodes = %d, want 25", cfg.MaxRetrievedNodes)
	}
	if cfg.VectorBackend != "in-memory" {
		t.Errorf("VectorBackend = %q, want in-memory", cfg.VectorBackend)
	}
	if cfg.VectorDim != 1536 {
		t.Errorf("VectorDim = %d, want 1536", cfg.VectorDim)
	}
}

// TestLoadConfigKeepsDefaultsOnInvalidValues enforces the "garbage in,
// default stays" rule from the parser: parser can't take the program
// down on bad config. Each invalid value stays at its default.
func TestLoadConfigKeepsDefaultsOnInvalidValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.ini")
	contents := `[ingestion]
dedup_threshold = not-a-number

[retrieval]
depth_ceiling = -3
max_nodes = abc
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DedupThreshold != 0.88 {
		t.Errorf("DedupThreshold on bad value = %v, want default 0.88", cfg.DedupThreshold)
	}
	// DepthCeiling=-3 fails the (err==nil && v>=0) guard, so the parser
	// logs "keeping default" and leaves the field at the initial
	// default (5). The parser is partial-recovery by design: invalid
	// input never takes the program down, it just doesn't replace the
	// crate's chosen safety ceiling.
	if cfg.MaxDepthCeiling != 5 {
		t.Errorf("MaxDepthCeiling on negative value = %d, want default 5", cfg.MaxDepthCeiling)
	}
	if cfg.MaxRetrievedNodes != 100 {
		t.Errorf("MaxRetrievedNodes on bad value = %d, want default 100", cfg.MaxRetrievedNodes)
	}
}

// TestLoadConfigSectionCaseInsensitive confirms that section and key
// names are matched case-insensitively. The parser lowercases the
// "section.key" string in its switch, so an INI written by hand with
// uppercase or mixed case still parses correctly. This is a config
// contract test: any regression here means existing operator-written
// configs silently lose their settings.
func TestLoadConfigSectionCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermem.ini")
	contents := `[EMBEDDER]
PROVIDER = OPENAI
URL = https://api.example.com/v1
Key = sk-mixed-case

[DATABASE]
Path = /tmp/caps.db

	[Extraction]
	Model = gpt-4o
	Temperature = 0.2

	[INGESTION]
dedup_threshold = 0.91

[RETRIEVAL]
depth_ceiling = 4
MAX_NODES = 50

[VECTOR]
DIM = 512
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", cfg.Provider)
	}
	if cfg.URL != "https://api.example.com/v1" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.Key != "sk-mixed-case" {
		t.Errorf("Key = %q, want sk-mixed-case", cfg.Key)
	}
	if cfg.DBPath != "/tmp/caps.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.ExtractModel != "gpt-4o" {
		t.Errorf("ExtractModel = %q, want gpt-4o", cfg.ExtractModel)
	}
	if cfg.ExtractTemperature != 0.2 {
		t.Errorf("ExtractTemperature = %v, want 0.2", cfg.ExtractTemperature)
	}
	if cfg.DedupThreshold != 0.91 {
		t.Errorf("DedupThreshold = %v, want 0.91", cfg.DedupThreshold)
	}
	if cfg.MaxDepthCeiling != 4 {
		t.Errorf("MaxDepthCeiling = %d, want 4", cfg.MaxDepthCeiling)
	}
	if cfg.MaxRetrievedNodes != 50 {
		t.Errorf("MaxRetrievedNodes = %d, want 50", cfg.MaxRetrievedNodes)
	}
	if cfg.VectorDim != 512 {
		t.Errorf("VectorDim = %d, want 512", cfg.VectorDim)
	}
}

func TestLoadConfigRetentionDefaults(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := LoadConfig(filepath.Join(tmp, "no-such.ini"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Retention.ObservationTTL != 90*24*time.Hour {
		t.Errorf("ObservationTTL = %v, want 2160h", cfg.Retention.ObservationTTL)
	}
	if cfg.Retention.RunInterval != 1*time.Hour {
		t.Errorf("RunInterval = %v, want 1h", cfg.Retention.RunInterval)
	}
	if cfg.Retention.DeleteBatchSize != 500 {
		t.Errorf("DeleteBatchSize = %d, want 500", cfg.Retention.DeleteBatchSize)
	}
}

func TestLoadConfigParsesRetentionKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermem.ini")
	contents := `[retention]
observation_ttl = 720h
run_interval = 30m
batch_size = 100
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Retention.ObservationTTL != 720*time.Hour {
		t.Errorf("ObservationTTL = %v, want 720h", cfg.Retention.ObservationTTL)
	}
	if cfg.Retention.RunInterval != 30*time.Minute {
		t.Errorf("RunInterval = %v, want 30m", cfg.Retention.RunInterval)
	}
	if cfg.Retention.DeleteBatchSize != 100 {
		t.Errorf("DeleteBatchSize = %d, want 100", cfg.Retention.DeleteBatchSize)
	}
}
func TestLoadConfigExtractionOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermem.ini")
	contents := `[embedder]
provider = ollama
url = http://ollama-host:11434
key = ollama-key

[extraction]
provider = openai
url = https://api.openai.com/v1
key = sk-test-extraction
model = gpt-4o
temperature = 0.2
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ExtractProvider != "openai" {
		t.Errorf("ExtractProvider = %q, want openai", cfg.ExtractProvider)
	}
	if cfg.ExtractURL != "https://api.openai.com/v1" {
		t.Errorf("ExtractURL = %q", cfg.ExtractURL)
	}
	if cfg.ExtractKey != "sk-test-extraction" {
		t.Errorf("ExtractKey = %q", cfg.ExtractKey)
	}
	if cfg.ExtractModel != "gpt-4o" {
		t.Errorf("ExtractModel = %q, want gpt-4o", cfg.ExtractModel)
	}
	// Embedder settings unchanged
	if cfg.Provider != "ollama" {
		t.Errorf("Provider = %q, want ollama", cfg.Provider)
	}
	if cfg.URL != "http://ollama-host:11434" {
		t.Errorf("URL = %q", cfg.URL)
	}
}

func TestLoadConfigExtractionFallsBackToEmbedder(t *testing.T) {
	// When extraction.provider/url/key are unset, NewExtractor should
	// inherit embedder values.
	cfg := &Config{
		Provider:          "openai",
		URL:               "https://api.openai.com/v1",
		Key:               "sk-embedder",
		ExtractModel:       "gpt-4o-mini",
		ExtractTemperature: 0.1,
	}
	// Use reflection-free check: create the extractor and inspect its type.
	// We can't easily inspect URL/Key of the returned interface, but
	// the test confirms it picks the right backend (openai vs ollama).
	ext := cfg.NewExtractor()
	if _, ok := ext.(*OpenAILLMExtractor); !ok {
		t.Errorf("NewExtractor with provider=openai returned %T, want *OpenAILLMExtractor", ext)
	}

	// With explicit extraction provider, it should override
	cfg2 := &Config{
		Provider:          "ollama",
		ExtractProvider:    "openai",
		ExtractURL:         "https://custom.openai.com",
		ExtractKey:         "sk-custom",
		ExtractModel:       "gpt-4o",
		ExtractTemperature: 0.3,
	}
	ext2 := cfg2.NewExtractor()
	if _, ok := ext2.(*OpenAILLMExtractor); !ok {
		t.Errorf("NewExtractor with ExtractProvider=openai returned %T, want *OpenAILLMExtractor", ext2)
	}
}

func TestLoadConfigVectorBackend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermem.ini")
	contents := `[database]
backend = sqlite-vec

[vector]
dim = 1024
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.VectorBackend != "sqlite-vec" {
		t.Errorf("VectorBackend = %q, want sqlite-vec", cfg.VectorBackend)
	}
	if cfg.VectorDim != 1024 {
		t.Errorf("VectorDim = %d, want 1024", cfg.VectorDim)
	}
}

// TestLoadConfigFromDir_Found drives the same code path as the
// production entry point LoadConfigFromBinaryDir, but with a
// caller-supplied directory so os.Executable() doesn't have to
// be faked (stdlib doesn't allow that). Verifies the binary-dir
// resolution contract: hermem.ini next to the binary is loaded,
// overriding defaults.
func TestLoadConfigFromDir_Found(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermem.ini")
	contents := `[embedder]
url = http://bin-dir-host:9999
model = bin-dir-model

[database]
path = bin-dir.db
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := LoadConfigFromDir(dir)
	if err != nil {
		t.Fatalf("LoadConfigFromDir: %v", err)
	}
	if cfg.URL != "http://bin-dir-host:9999" {
		t.Errorf("URL = %q, want http://bin-dir-host:9999", cfg.URL)
	}
	if cfg.Model != "bin-dir-model" {
		t.Errorf("Model = %q, want bin-dir-model", cfg.Model)
	}
	if cfg.DBPath != "bin-dir.db" {
		t.Errorf("DBPath = %q, want bin-dir.db", cfg.DBPath)
	}
}

// TestLoadConfigFromDir_MissingReturnsDefaults confirms that an
// absent hermem.ini near the binary silently falls back to the
// built-in defaults (matching LoadConfig's existing policy),
// rather than surfacing an error and aborting startup. This is
// the operator-facing guarantee behind the acceptance criterion
// "an empty CWD no longer creates a stray hermem.db" — the
// binary boots cleanly with defaults even without ini.
func TestLoadConfigFromDir_MissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir() // intentionally no hermem.ini in here
	cfg, err := LoadConfigFromDir(dir)
	if err != nil {
		t.Fatalf("LoadConfigFromDir: %v", err)
	}
	if cfg.URL != "http://localhost:11434" {
		t.Errorf("URL = %q, want default http://localhost:11434", cfg.URL)
	}
	if cfg.Model != "nomic-embed-text" {
		t.Errorf("Model = %q, want default nomic-embed-text", cfg.Model)
	}
	if cfg.DBPath != "hermem.db" {
		t.Errorf("DBPath = %q, want default hermem.db", cfg.DBPath)
	}
}
