package migration

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

// TestToStatusPreservesFields pins that the store→domain adapter for the
// moved Status type carries every field verbatim (including the nullable
// ChecksumMatch pointer), so the core→migration ownership move did not drop
// any wire-visible data.
func TestToStatusPreservesFields(t *testing.T) {
	match := false
	in := []store.MigStatus{{
		Name:           "0001_init.sql",
		Applied:        true,
		AppliedAt:      "2026-01-01T00:00:00Z",
		ChecksumSHA256: "abc123",
		ChecksumMatch:  &match,
	}}
	out := toStatus(in)
	if len(out) != 1 {
		t.Fatalf("len: got %d, want 1", len(out))
	}
	got := out[0]
	if got.Name != "0001_init.sql" || !got.Applied || got.AppliedAt != "2026-01-01T00:00:00Z" ||
		got.ChecksumSHA256 != "abc123" || got.ChecksumMatch == nil || *got.ChecksumMatch != false {
		t.Fatalf("status mapping drifted: %+v", got)
	}
}

// TestToMismatchPreservesFields pins the moved Mismatch adapter carries the
// stored/current checksum pair verbatim.
func TestToMismatchPreservesFields(t *testing.T) {
	in := []store.MigMismatch{{
		Name:            "0001_init.sql",
		StoredChecksum:  "stored",
		CurrentChecksum: "current",
	}}
	out := toMismatch(in)
	if len(out) != 1 {
		t.Fatalf("len: got %d, want 1", len(out))
	}
	got := out[0]
	if got.Name != "0001_init.sql" || got.StoredChecksum != "stored" || got.CurrentChecksum != "current" {
		t.Fatalf("mismatch mapping drifted: %+v", got)
	}
}
