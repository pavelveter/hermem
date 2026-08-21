package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteOwnerOnly_FreshInstall_SmokeMode is the SMOKE canary: a
// fresh install produces a 0o600 file from open(2) creating at the
// explicit-mode argument alone. This test would pass even if the
// post-WriteFile Chmod were accidentally removed. The actual
// correctness canary is TestWriteOwnerOnly_LegacyUpgrade_NarrowsMode
// below — that one distinguishes the post-Chmod from bare WriteFile.
// Both kept: smoke catches gross mode regressions; canary catches
// silent post-Chmod removal.
func TestWriteOwnerOnly_FreshInstall_SmokeMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "first.json")

	if err := WriteOwnerOnly(path, []byte(`{"k":"v"}`)); err != nil {
		t.Fatalf("WriteOwnerOnly: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("fresh install: mode got %#o, want %#o", got, want)
	}
}

// TestWriteOwnerOnly_LegacyUpgrade_NarrowsMode is the CORRECTNESS
// CANARY for the post-WriteFile Chmod. A 0o644 fixture file
// (simulating an upgraded-from-0.3.x install) MUST be actively
// narrowed to 0o600 by the next WriteOwnerOnly call. Plain
// os.WriteFile does NOT narrow (open(2)'s mode argument is only
// consulted on file CREATION); the post-WriteFile Chmod is what
// closes that migration gap. If this test fails, the post-Chmod
// was removed.
func TestWriteOwnerOnly_LegacyUpgrade_NarrowsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")

	if err := os.WriteFile(path, []byte("legacy 0o644 seed"), 0o644); err != nil {
		t.Fatalf("seed legacy 0o644: %v", err)
	}

	if err := WriteOwnerOnly(path, []byte(`{"k":"v"}`)); err != nil {
		t.Fatalf("WriteOwnerOnly: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("upgrade-mutation: mode got %#o, want %#o", got, want)
	}
}

// TestWriteOwnerOnly_ContentPreserved asserts the byte stream the
// caller passed reaches disk verbatim. Trivial correctness check that
// guards against accidental truncation or any future encoding layer
// introduced into the helper.
func TestWriteOwnerOnly_ContentPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")

	// A string with no JSON structural meaning — guards against any
	// future mJSON-encode / pretty-print refactor that would change
	// the byte content.
	expected := []byte("Lorem ipsum 12345 — not JSON, not UTF-8-bom.")
	if err := WriteOwnerOnly(path, expected); err != nil {
		t.Fatalf("WriteOwnerOnly: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(expected) {
		t.Errorf("content drift: got %q (len=%d), want %q (len=%d)",
			got, len(got), expected, len(expected))
	}
}
