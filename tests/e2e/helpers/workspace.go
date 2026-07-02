package helpers

import (
	"os"
	"path/filepath"
	"testing"
)

// TempWorkspace creates a temporary directory for test isolation.
// Returns the path and a cleanup function.
func TempWorkspace(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "hermem-e2e-*")
	if err != nil {
		t.Fatalf("create temp workspace: %v", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	t.Cleanup(cleanup)
	return dir, cleanup
}

// WriteConfig writes a hermem.ini file to the given directory.
func WriteConfig(t *testing.T, dir string, content string) {
	t.Helper()
	path := filepath.Join(dir, "hermem.ini")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// WriteConfigForCLI writes hermem.ini into the per-test workspace.
//
// Historically this helper also wrote to `filepath.Dir(BinaryPath(t))/hermem.ini`
// (the binary's install directory) so the hermem CLI subprocess could
// find the config when launched without flags. That shared write was the
// source of a cross-test race: every e2e package and every subtest
// clobbered the SAME `binDir/hermem.ini` path. Concurrent `go test
// ./tests/e2e/...` packages saw each other's config content mid-run.
//
// The clean fix is to keep config entirely within `dir` and tell the
// hermem subprocess where to find it via the `HERMEM_INI` env var
// (which src/internal/config.LoadConfigFromSources honors at the second
// precedence tier, just below `--config` flag). NewCLI and StartServer
// set `HERMEM_INI=<dir>/hermem.ini` on their subprocess env, so the
// binDir write is no longer required. We deliberately do NOT write to
// binDir here.
func WriteConfigForCLI(t *testing.T, dir string, content string) {
	t.Helper()
	WriteConfig(t, dir, content)
}

// DefaultConfig returns a minimal hermem.ini for testing.
func DefaultConfig(dbPath string) string {
	return "[database]\npath = " + dbPath + "\nbackend = in-memory\nauto_migrate = true\n"
}

// TaskConfig returns a hermem.ini with task stateful category enabled.
func TaskConfig(dbPath string) string {
	return DefaultConfig(dbPath) + "\n[schema]\nallowed_categories = world, opinion, experience, observation, task\nallowed_relations = related_to, blocked_by, recovers_via, part_of, causes, contradicts, prefers, uses, mentions\nstateful_categories = task\nvalid_states = pending, in_progress, done\nstate_unblocking = done\n"
}

// DBPath returns the path to the SQLite database in the workspace.
func DBPath(dir string) string {
	return filepath.Join(dir, "hermem.db")
}
