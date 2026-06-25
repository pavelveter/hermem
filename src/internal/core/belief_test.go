package core

import (
	"reflect"
	"testing"
	"time"
)

// TestEntityToBelief_DropsUnrelated locks: Entity → Belief must drop
// every field that is not CreatedAt / UpdatedAt / LastAccessedAt /
// Archived / Degree. The 14 non-belief fields belong to Fact,
// Evidence, Episode, Task, and ID+Category — letting any of those
// leak through here would mean callers see memory-anchor-shape data
// carrying semantic content, quality meta, episode provenance, or
// task lifecycle, which would be a category error.
func TestEntityToBelief_DropsUnrelated(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)
	earlier := now.Add(-time.Hour)
	e := Entity{
		ID:             "b-1",
		Category:       "world",
		Content:        "Paris is the capital of France",
		Embedding:      []float32{0.1, 0.2, 0.3},
		Status:         "completed",
		Confidence:     0.95,
		Source:         "dialog",
		SourceType:     "extracted",
		CreatedAt:      &earlier,
		UpdatedAt:      now,
		LastAccessedAt: &later,
		Archived:       false,
		ValidFrom:      &earlier,
		ValidTo:        &later,
		Degree:         7,
		Priority:       3,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ExtractedFrom:  "msg-1",
	}
	got := e.AsBelief()
	want := Belief{
		CreatedAt:      &earlier,
		UpdatedAt:      now,
		LastAccessedAt: &later,
		Archived:       false,
		Degree:         7,
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("AsBelief(): want %+v, got %+v", want, got)
	}
}

// TestBeliefToEntity_ZerosUnrelated locks: Belief → Entity must zero
// / nil out every non-belief field (14 fields). Pointer-identity
// must be preserved on CreatedAt / LastAccessedAt so the tests
// aren't fooled by zero-time values.
func TestBeliefToEntity_ZerosUnrelated(t *testing.T) {
	now := time.Now()
	later := now.Add(2 * time.Hour)
	earlier := now.Add(-2 * time.Hour)
	b := Belief{
		CreatedAt:      &earlier,
		UpdatedAt:      now,
		LastAccessedAt: &later,
		Archived:       true,
		Degree:         5,
	}
	e := b.AsEntity()

	// Belief fields preserved (pointer-identity must match).
	if e.CreatedAt != b.CreatedAt {
		t.Errorf("CreatedAt pointer lost: want %p got %p", b.CreatedAt, e.CreatedAt)
	}
	if !e.UpdatedAt.Equal(b.UpdatedAt) {
		t.Errorf("UpdatedAt lost: want %v got %v", b.UpdatedAt, e.UpdatedAt)
	}
	if e.LastAccessedAt != b.LastAccessedAt {
		t.Errorf("LastAccessedAt pointer lost: want %p got %p", b.LastAccessedAt, e.LastAccessedAt)
	}
	if e.Archived != b.Archived {
		t.Errorf("Archived lost: want %v got %v", b.Archived, e.Archived)
	}
	if e.Degree != b.Degree {
		t.Errorf("Degree lost: want %d got %d", b.Degree, e.Degree)
	}

	// 14 unrelated fields must be at their zero / nil defaults.
	if e.ID != "" || e.Category != "" || e.Content != "" {
		t.Errorf("Fact fields not zero: id=%q category=%q content=%q", e.ID, e.Category, e.Content)
	}
	if e.Embedding != nil {
		t.Errorf("Embedding not nil: %v", e.Embedding)
	}
	if e.Confidence != 0 || e.Source != "" || e.SourceType != "" {
		t.Errorf("Evidence fields not zero: confidence=%v source=%q source_type=%q", e.Confidence, e.Source, e.SourceType)
	}
	if e.Status != "" {
		t.Errorf("Task/Status not zero: %q", e.Status)
	}
	if e.ValidFrom != nil || e.ValidTo != nil {
		t.Errorf("validity window not nil: %+v %+v", e.ValidFrom, e.ValidTo)
	}
	if e.Priority != 0 {
		t.Errorf("Priority not zero: %d", e.Priority)
	}
	if e.ConversationID != "" || e.MessageID != "" || e.ExtractedFrom != "" {
		t.Errorf("Episode fields not zero: %+v", e)
	}
}

// TestBelief_RoundTrip_Exact locks: Belief → Entity → Belief is a
// clean round-trip on the 5 belief fields, including pointer-identity
// for the *time.Time timestamps.
func TestBelief_RoundTrip_Exact(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)
	earlier := now.Add(-time.Hour)
	original := Belief{
		CreatedAt:      &earlier,
		UpdatedAt:      now,
		LastAccessedAt: &later,
		Archived:       true,
		Degree:         4,
	}
	got := original.AsEntity().AsBelief()
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("belief round-trip shifted fields: want %+v, got %+v", original, got)
	}
}

// TestEntityBelief_RoundTrip_Lossy locks: Entity → Belief → Entity
// preserves the 5 belief fields and drops the 14 unrelated fields.
// Documents the deliberately lossy upward path.
func TestEntityBelief_RoundTrip_Lossy(t *testing.T) {
	now := time.Now()
	later := now.Add(7 * 24 * time.Hour)
	earlier := now.Add(-30 * 24 * time.Hour)
	original := Entity{
		ID:             "b-4",
		Category:       "world",
		Content:        "lossy",
		Embedding:      []float32{0.1},
		Status:         "running",
		Confidence:     0.9,
		Source:         "dialog",
		SourceType:     "extracted",
		CreatedAt:      &earlier,
		UpdatedAt:      now,
		LastAccessedAt: &later,
		Archived:       true,
		ValidFrom:      &earlier,
		ValidTo:        &later,
		Degree:         5,
		Priority:       2,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ExtractedFrom:  "msg-1",
	}
	roundTripped := original.AsBelief().AsEntity()

	// Belief fields preserved.
	if roundTripped.CreatedAt != original.CreatedAt {
		t.Errorf("CreatedAt pointer lost: want %p got %p", original.CreatedAt, roundTripped.CreatedAt)
	}
	if !roundTripped.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt lost: want %v got %v", original.UpdatedAt, roundTripped.UpdatedAt)
	}
	if roundTripped.LastAccessedAt != original.LastAccessedAt {
		t.Errorf("LastAccessedAt pointer lost: want %p got %p", original.LastAccessedAt, roundTripped.LastAccessedAt)
	}
	if roundTripped.Archived != original.Archived {
		t.Errorf("Archived lost: want %v got %v", original.Archived, roundTripped.Archived)
	}
	if roundTripped.Degree != original.Degree {
		t.Errorf("Degree lost: want %d got %d", original.Degree, roundTripped.Degree)
	}

	// 14 unrelated fields intentionally dropped.
	if roundTripped.ID != "" || roundTripped.Category != "" || roundTripped.Content != "" {
		t.Errorf("Fact fields not zero: %+v", roundTripped)
	}
	if roundTripped.Confidence != 0 || roundTripped.Source != "" {
		t.Errorf("Evidence fields not zero: %+v", roundTripped)
	}
	if roundTripped.Status != "" {
		t.Errorf("Task/Status not zero: %q", roundTripped.Status)
	}
	if roundTripped.ConversationID != "" {
		t.Errorf("Episode field not zero: ConversationID=%q", roundTripped.ConversationID)
	}
}
