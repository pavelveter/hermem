package graph

import (
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// TestSafeSourceLabel_NilReturnsCanonical — nil pointer must NOT panic.
func TestSafeSourceLabel_NilReturnsCanonical(t *testing.T) {
	if got := SafeSourceLabel(nil); got != "unknown_or_deleted_source" {
		t.Fatalf("want canonical sentinel, got %q", got)
	}
}

// TestSafeSourceLabel_EmptyIDReturnsCanonical — even with a non-nil
// pointer, an empty ID is treated as "deleted" (archived rows often
// leave behind empty PK references during in-flight updates).
func TestSafeSourceLabel_EmptyIDReturnsCanonical(t *testing.T) {
	if got := SafeSourceLabel(&Source{ID: "", Label: "ghost"}); got != "unknown_or_deleted_source" {
		t.Fatalf("want canonical sentinel, got %q", got)
	}
}

// TestSafeSourceLabel_NormalPath — happy path surfaces s.Label.
func TestSafeSourceLabel_NormalPath(t *testing.T) {
	if got := SafeSourceLabel(&Source{ID: "src-1", Label: "user"}); got != "user" {
		t.Fatalf("want user, got %q", got)
	}
}

// TestWalkLineage_Baseline — slices through nodes 1:1 mapping each to
// a LineageEntry that carries SourceTag and UpdatedAt.
func TestWalkLineage_Baseline(t *testing.T) {
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	nodes := []core.Entity{
		{ID: "f1", Source: "user", UpdatedAt: core.TimePtr(now)},
		{ID: "f2", Source: "", UpdatedAt: core.TimePtr(now.Add(1 * time.Minute))}, // deleted source
	}
	got := WalkLineage(nodes)
	if len(got) != 2 {
		t.Fatalf("len: want 2, got %d", len(got))
	}
	if got[0].FactID != "f1" || got[0].SourceTag != "user" {
		t.Fatalf("idx 0: %+v", got[0])
	}
	if got[1].FactID != "f2" || got[1].SourceTag != "unknown_or_deleted_source" {
		t.Fatalf("idx 1 (deleted source): %+v", got[1])
	}
}

// TestWalkLineage_EmptyInput — empty input returns a non-nil empty slice
// (callers often range over it without nil checks).
func TestWalkLineage_EmptyInput(t *testing.T) {
	got := WalkLineage(nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %v", got)
	}
}
