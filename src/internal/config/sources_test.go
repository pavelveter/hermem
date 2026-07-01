package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeINIfile writes content to a hermem.ini inside a fresh temp dir
// and returns its absolute path. Helpers write files with a unique
// db_path so the test can assert which file got loaded by reading
// cfg.DBPath.
func writeINIfile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hermem.ini")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write hermem.ini fixture: %v", err)
	}
	return path
}

// TestLoadConfigFromSources_FlagWinsOverEnv — the headline behavior.
// When --config is set AND HERMEM_INI is set, the flag wins. The
// sentinel value chosen here is unambiguous (no chance of confusing
// with another db_path literal in the codebase), so a regression that
// re-orders precedence would surface as a clear test failure rather than
// a value-equality slip-through.
func TestLoadConfigFromSources_FlagWinsOverEnv(t *testing.T) {
	flagPath := writeINIfile(t, "[database]\npath = flag-sentinel-xyz.db\n")
	envPath := writeINIfile(t, "[database]\npath = env-leaked-fallback.db\n")
	t.Setenv("HERMEM_INI", envPath)

	cfg, err := LoadConfigFromSources(flagPath)
	if err != nil {
		t.Fatalf("LoadConfigFromSources(flagPath): %v", err)
	}
	if cfg.DBPath != "flag-sentinel-xyz.db" {
		t.Errorf("flag should win over HERMEM_INI; got DBPath=%q, want flag-sentinel-xyz.db", cfg.DBPath)
	}
}

// TestLoadConfigFromSources_EnvWinsWhenFlagEmpty — flag="" falls through
// to HERMEM_INI (treats empty as unset, does NOT short-circuit to a
// default).
func TestLoadConfigFromSources_EnvWinsWhenFlagEmpty(t *testing.T) {
	envPath := writeINIfile(t, "[database]\npath = env-db.db\n")
	t.Setenv("HERMEM_INI", envPath)

	cfg, err := LoadConfigFromSources("")
	if err != nil {
		t.Fatalf("LoadConfigFromSources(\"\"): %v", err)
	}
	if cfg.DBPath != "env-db.db" {
		t.Errorf("empty flag should fall through to env; got DBPath=%q, want env-db.db", cfg.DBPath)
	}
}

// TestLoadConfigFromBinaryDir_ShimContract — legacy LoadConfigFromBinaryDir
// must produce the same DBPath as LoadConfigFromSources("") when HERMEM_INI
// is set. Locks the shim contract so a future "improvement" to either
// function can't silently diverge them.
func TestLoadConfigFromBinaryDir_ShimContract(t *testing.T) {
	envPath := writeINIfile(t, "[database]\npath = shim-contract-db.db\n")
	t.Setenv("HERMEM_INI", envPath)

	shimCfg, err := LoadConfigFromBinaryDir()
	if err != nil {
		t.Fatalf("LoadConfigFromBinaryDir: %v", err)
	}
	mainCfg, err := LoadConfigFromSources("")
	if err != nil {
		t.Fatalf("LoadConfigFromSources(\"\"): %v", err)
	}
	if shimCfg.DBPath != mainCfg.DBPath {
		t.Errorf("shim contract broken: LoadConfigFromBinaryDir=%q, LoadConfigFromSources(\"\")=%q", shimCfg.DBPath, mainCfg.DBPath)
	}
}

// TestLoadConfigFromSources_BinaryDirFallback_NoError — when no flag, no
// env, LoadConfigFromSources falls through to os.Executable()'s dir.
// We don't override os.Executable(); if that directory has no
// hermem.ini, LoadConfigFromDir returns defaults. We assert the call
// returns non-nil, no error — i.e. the fallback branch is reachable.
//
// Note: this test runs against the test binary's directory (because
// os.Executable returns the running test binary). On CI/dev machines,
// that directory typically has no hermem.ini, so defaults come back.
// To make the test deterministic regardless of the local environment,
// we ALSO write a hermem.ini next to the test binary via a symlink in
// t.TempDir() — but that requires non-portable symlink shims, so we
// stick with the "returns defaults without panic" assertion which is
// what we actually want to guard.
func TestLoadConfigFromSources_BinaryDirFallback_NoError(t *testing.T) {
	t.Setenv("HERMEM_INI", "")

	cfg, err := LoadConfigFromSources("")
	if err != nil {
		t.Fatalf("LoadConfigFromSources(\"\"): binary-dir fallback should succeed when no hermem.ini is present; got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil Config when falling back to binary-dir (defaults path)")
	}
	// DBPath must be set to something (default = "hermem.db" per
	// defaultConfig). If a real hermem.ini were loaded, DBPath
	// could be anything; defaultConfig pins "hermem.db".
	if cfg.DBPath != "hermem.db" {
		t.Logf("binary-dir fallback loaded a non-default DBPath=%q", cfg.DBPath)
		// Don't fail — this just means there IS a hermem.ini next to
		// the test binary, which is fine. The contract is "doesn't
		// panic and returns a Config".
	}
}

// TestLoadConfigFromSources_LoadErrorPropagates — if the --config path
// can't even be parsed, the error must reach the caller (we don't
// silently fall through to env). This guards the precedence-1 branch.
func TestLoadConfigFromSources_LoadErrorPropagatesFlagPath(t *testing.T) {
	// A directory at the flag path causes os.Open to succeed (it's
	// readable) but ini.Load to fail because the directory is not a
	// valid INI file. The error must reach the caller.
	dir := t.TempDir()
	t.Setenv("HERMEM_INI", "")

	if _, err := LoadConfigFromSources(dir); err == nil {
		t.Fatal("expected error when --config path is a directory; got nil")
	}
}
