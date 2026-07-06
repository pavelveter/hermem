package config

import (
	"os"
	"path/filepath"
	"testing"
)

// baseConfigWithKey writes a minimal hermem.ini with one api_keys line so the
// rotation/removal tests have something to act on. Mode is 0o600 by
// construction — this helper does not regress to 0o644 under any test path.
func baseConfigWithKey(t *testing.T, label string) string {
	t.Helper()
	dir := t.TempDir()
	ini := filepath.Join(dir, "hermem.ini")
	body := "[server]\napi_keys = sk-initial:scope:" + label + "\n"
	if err := os.WriteFile(ini, []byte(body), 0o600); err != nil {
		t.Fatalf("seed ini: %v", err)
	}
	return ini
}

// TestAddKeyToFile_SetsOwnerOnlyMode asserts that even on a brand-new file
// (no prior content), the resulting hermem.ini has mode 0o600, not 0o644.
// This is the regression: the previous implementation called
// os.WriteFile(..., 0644), which made every newly-added api_key
// world-readable on any local user account.
func TestAddKeyToFile_SetsOwnerOnlyMode(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "hermem.ini")

	if err := AddKeyToFile(ini, "sk-test-key", "scope", "label1"); err != nil {
		t.Fatalf("AddKeyToFile: %v", err)
	}

	info, err := os.Stat(ini)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode after AddKeyToFile: got %#o, want %#o", got, want)
	}
}

// TestRotateKeyInFile_SetsOwnerOnlyMode asserts that rotating an existing
// key does not regress the file mode. Previously this site called
// os.WriteFile(..., 0644) which silently downgraded an already-restricted
// file on every key rotation.
func TestRotateKeyInFile_SetsOwnerOnlyMode(t *testing.T) {
	ini := baseConfigWithKey(t, "label1")

	if err := RotateKeyInFile(ini, "label1", "sk-rotated"); err != nil {
		t.Fatalf("RotateKeyInFile: %v", err)
	}

	info, err := os.Stat(ini)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode after RotateKeyInFile: got %#o, want %#o", got, want)
	}
}

// TestRemoveKeyFromFile_SetsOwnerOnlyMode asserts that removing an api_key
// does not regress the file mode. Symmetric to the rotation case: every
// mutator must end with a restriction-confirming write.
func TestRemoveKeyFromFile_SetsOwnerOnlyMode(t *testing.T) {
	ini := baseConfigWithKey(t, "label1")

	if err := RemoveKeyFromFile(ini, "label1"); err != nil {
		t.Fatalf("RemoveKeyFromFile: %v", err)
	}

	info, err := os.Stat(ini)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode after RemoveKeyFromFile: got %#o, want %#o", got, want)
	}
}

// TestAddKeyToFile_OnExistingFile_NarrowsMode asserts that the security
// posture is enforced even when the file pre-exists in a relaxed mode.
// This is the upgrade path: an operator who already has a 0o644 hermem.ini
// must see it tightened to 0o600 on the next mutating call.
func TestAddKeyToFile_OnExistingFile_NarrowsMode(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, "herhem.ini")
	body := "[server]\napi_keys = sk-pre-existing:scope:legacy\n"
	if err := os.WriteFile(ini, []byte(body), 0o644); err != nil {
		t.Fatalf("seed ini: %v", err)
	}

	if err := AddKeyToFile(ini, "sk-new", "scope", "newlabel"); err != nil {
		t.Fatalf("AddKeyToFile: %v", err)
	}

	info, err := os.Stat(ini)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode after upgrade-mutation: got %#o, want %#o", got, want)
	}
}
