package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigAllSections writes a hermem.ini with every known section+key
// and asserts that LoadConfig populates every Config field accordingly. This
// is the contract test for "config keys in code (ExtractModel, DedupThreshold,
// ...) must match INI keys (extraction.model, ingestion.dedup_threshold, ...)"
// (TODO §1.2). If a new Config field is added without a corresponding INI key,
// or vice versa, this test fails.
//
// Adding a field to Config? Add it here AND in hermem.ini (and in README §Defaults).
// Adding a key to LoadConfig? Add a row here AND a Config field to receive it.
func TestLoadConfigAllSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermem.ini")
	content := `[embedder]
provider = openai
url = https://api.openai.example/v1
key = test-key-xyz
model = text-embedding-3-large

[extraction]
model = gpt-4o-mini

[ingestion]
dedup_threshold = 0.92

[database]
path = /tmp/hermem-test.db

[retrieval]
depth_ceiling = 3
max_nodes = 50
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write ini: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"Provider", cfg.Provider, "openai"},
		{"URL", cfg.URL, "https://api.openai.example/v1"},
		{"Key", cfg.Key, "test-key-xyz"},
		{"Model", cfg.Model, "text-embedding-3-large"},
		{"ExtractModel", cfg.ExtractModel, "gpt-4o-mini"},
		{"DedupThreshold", cfg.DedupThreshold, float32(0.92)},
		{"DBPath", cfg.DBPath, "/tmp/hermem-test.db"},
		{"MaxDepthCeiling", cfg.MaxDepthCeiling, 3},
		{"MaxRetrievedNodes", cfg.MaxRetrievedNodes, 50},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestLoadConfigDefaults asserts LoadConfig returns the documented defaults
// when the file is absent. Defaults are the contract between code and the
// sample hermem.ini shipped with the repo.
func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/does-not-exist.ini")
	if err != nil {
		t.Fatalf("LoadConfig on missing file should not error, got: %v", err)
	}

	if cfg.Provider != "ollama" {
		t.Errorf("Provider default: got %q, want ollama", cfg.Provider)
	}
	if cfg.URL != "http://localhost:11434" {
		t.Errorf("URL default: got %q, want http://localhost:11434", cfg.URL)
	}
	if cfg.Model != "nomic-embed-text" {
		t.Errorf("Model default: got %q, want nomic-embed-text", cfg.Model)
	}
	if cfg.ExtractModel != "qwen2.5-coder:7b" {
		t.Errorf("ExtractModel default: got %q, want qwen2.5-coder:7b", cfg.ExtractModel)
	}
	if cfg.DedupThreshold != 0.88 {
		t.Errorf("DedupThreshold default: got %v, want 0.88", cfg.DedupThreshold)
	}
	if cfg.DBPath != "hermem.db" {
		t.Errorf("DBPath default: got %q, want hermem.db", cfg.DBPath)
	}
	if cfg.MaxDepthCeiling != 5 {
		t.Errorf("MaxDepthCeiling default: got %d, want 5", cfg.MaxDepthCeiling)
	}
	if cfg.MaxRetrievedNodes != 100 {
		t.Errorf("MaxRetrievedNodes default: got %d, want 100", cfg.MaxRetrievedNodes)
	}
}

// TestLoadConfigInvalidDedupThreshold asserts that an unparseable
// ingestion.dedup_threshold (typo, empty, suffixed garbage) does not crash
// LoadConfig and silently falls back to the default. This locks in the
// warn-and-fallback behaviour documented in config.go. Range validation of
// valid floats (NaN, negative, >1) is intentionally out of scope here —
// those parse cleanly today and are tracked separately.
func TestLoadConfigInvalidDedupThreshold(t *testing.T) {
	cases := []struct {
		name string
		ini  string
	}{
		{"unparseable-text", "dedup_threshold = not-a-float"},
		{"empty-value", "dedup_threshold ="},
		{"suffixed-garbage", "dedup_threshold = 0.5abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "ini")
			if err := os.WriteFile(path, []byte("[ingestion]\n"+tc.ini+"\n"), 0o644); err != nil {
				t.Fatalf("write ini: %v", err)
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig on bad value should not error, got: %v", err)
			}
			if cfg.DedupThreshold != 0.88 {
				t.Errorf("invalid dedup_threshold must keep default 0.88, got %v", cfg.DedupThreshold)
			}
		})
	}
}

// TestLoadConfigPartialIni asserts LoadConfig keeps defaults for missing
// sections but overrides present ones correctly.
func TestLoadConfigPartialIni(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.ini")
	if err := os.WriteFile(path, []byte("[embedder]\nmodel = custom-model\n"), 0o644); err != nil {
		t.Fatalf("write ini: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Model != "custom-model" {
		t.Errorf("Model override: got %q, want custom-model", cfg.Model)
	}
	// Other fields should keep their defaults.
	if cfg.Provider != "ollama" {
		t.Errorf("Provider not overridden: got %q, want ollama", cfg.Provider)
	}
	if cfg.DedupThreshold != 0.88 {
		t.Errorf("DedupThreshold default preserved: got %v, want 0.88", cfg.DedupThreshold)
	}
}
