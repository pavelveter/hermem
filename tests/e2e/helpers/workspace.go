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

// WriteConfigForCLI writes hermem.ini next to the binary (where the CLI reads it)
// and registers cleanup to remove it after the test.
func WriteConfigForCLI(t *testing.T, dir string, content string) {
	t.Helper()
	WriteConfig(t, dir, content)
	binDir := filepath.Dir(BinaryPath(t))
	binPath := filepath.Join(binDir, "hermem.ini")
	if err := os.WriteFile(binPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config to bin dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(binPath) })
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
