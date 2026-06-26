package core

import (
	"reflect"
	"testing"
	"time"
)

// TestEntityToEpisode_DropsUnrelated locks: Entity → Episode must
// drop every field that is not ConversationID / MessageID /
// ExtractedFrom. This is the canonical projection boundary — letting
// any unrelated field bleed through here means callers see episode-shape
// data carrying fact content, evidence provenance, or task lifecycle,
// which would be a category error.
func TestEntityToEpisode_DropsUnrelated(t *testing.T) {
	now := time.Now()
	e := Entity{
		ID:             "e-1",
		Category:       "world",
		Content:        "Paris is the capital of France",
		Embedding:      []float32{0.1, 0.2, 0.3},
		Status:         "completed",
		Confidence:     0.95,
		Source:         "dialog",
		SourceType:     "extracted",
		CreatedAt:      &now,
		UpdatedAt:      &now,
		LastAccessedAt: &now,
		Archived:       true,
		ValidFrom:      &now,
		ValidTo:        &now,
		Degree:         7,
		Priority:       3,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ExtractedFrom:  "msg-1",
	}
	got := e.AsEpisode()
	want := Episode{
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ExtractedFrom:  "msg-1",
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("AsEpisode(): want %+v, got %+v", want, got)
	}
}

// TestEpisodeToEntity_ZerosUnrelated locks: Episode → Entity must
// zero / nil out every non-episode field. Callers that need them
// back must compose with another domain model (Fact, Evidence, Task).
func TestEpisodeToEntity_ZerosUnrelated(t *testing.T) {
	ep := Episode{
		ConversationID: "conv-2",
		MessageID:      "msg-2",
		ExtractedFrom:  "msg-2",
	}
	e := Compose(Fact{}, Evidence{}, ep, Task{}, Belief{})

	// Episode fields preserved.
	if e.ConversationID != ep.ConversationID || e.MessageID != ep.MessageID || e.ExtractedFrom != ep.ExtractedFrom {
		t.Fatalf("episode fields lost: got %+v", e)
	}

	// 16 unrelated fields must be at their zero / nil defaults.
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
	if e.CreatedAt != nil || e.UpdatedAt != nil || e.LastAccessedAt != nil {
		t.Errorf("timestamp fields not zero/nil: %+v %+v %+v", e.CreatedAt, e.UpdatedAt, e.LastAccessedAt)
	}
	if e.Archived || e.Degree != 0 || e.Priority != 0 {
		t.Errorf("graph fields not zero: archived=%v degree=%d priority=%d", e.Archived, e.Degree, e.Priority)
	}
}

// TestEpisode_RoundTrip_Exact locks: Episode → Entity → Episode is a
// clean round-trip on the 3 episode fields.
func TestEpisode_RoundTrip_Exact(t *testing.T) {
	original := Episode{
		ConversationID: "conv-3",
		MessageID:      "msg-3",
		ExtractedFrom:  "msg-3",
	}
	got := Compose(Fact{}, Evidence{}, original, Task{}, Belief{}).AsEpisode()
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("episode round-trip shifted fields: want %+v, got %+v", original, got)
	}
}

// TestEntityEpisode_RoundTrip_Lossy locks: Entity → Episode → Entity
// preserves the 3 episode fields exactly and drops the 16 unrelated
// fields. Documents the deliberately lossy upward path.
func TestEntityEpisode_RoundTrip_Lossy(t *testing.T) {
	now := time.Now()
	original := Entity{
		ID:             "e-4",
		Category:       "world",
		Content:        "lossy",
		Embedding:      []float32{0.1},
		Status:         "running",
		Confidence:     0.9,
		Source:         "dialog",
		SourceType:     "extracted",
		CreatedAt:      &now,
		UpdatedAt:      &now,
		LastAccessedAt: &now,
		Archived:       true,
		ValidFrom:      &now,
		ValidTo:        &now,
		Degree:         5,
		Priority:       2,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ExtractedFrom:  "msg-1",
	}
	roundTripped := Compose(Fact{}, Evidence{}, original.AsEpisode(), Task{}, Belief{})

	// Episode fields preserved.
	if roundTripped.ConversationID != original.ConversationID ||
		roundTripped.MessageID != original.MessageID ||
		roundTripped.ExtractedFrom != original.ExtractedFrom {
		t.Fatalf("episode fields lost: want %+v, got %+v", original, roundTripped)
	}

	// 16 unrelated fields intentionally dropped.
	if roundTripped.ID != "" || roundTripped.Category != "" || roundTripped.Content != "" {
		t.Errorf("Fact fields not zero: %+v", roundTripped)
	}
	if roundTripped.Confidence != 0 || roundTripped.Source != "" || roundTripped.SourceType != "" {
		t.Errorf("Evidence fields not zero: %+v", roundTripped)
	}
	if roundTripped.Status != "" {
		t.Errorf("Task/Status not zero: %q", roundTripped.Status)
	}
}
