package core

import (
	"reflect"
	"testing"
	"time"
)

// TestEntityToEvidence_DropsUnrelated locks: Entity → Evidence must
// drop every field that is not Confidence / Source / SourceType.
// This is the canonical projection boundary — letting any unrelated
// field bleed through here means callers see evidence-shape data
// carrying task state, retention timestamps, or episode provenance,
// which would be a category error.
func TestEntityToEvidence_DropsUnrelated(t *testing.T) {
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
		UpdatedAt:      now,
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
	got := e.AsEvidence()
	want := Evidence{
		Confidence: 0.95,
		Source:     "dialog",
		SourceType: "extracted",
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("AsEvidence(): want %+v, got %+v", want, got)
	}
}

// TestEvidenceToEntity_ZerosUnrelated locks: Evidence → Entity must
// zero / nil out every non-evidence field. Callers that need them
// back must compose with another domain model (Fact, Episode, Task).
func TestEvidenceToEntity_ZerosUnrelated(t *testing.T) {
	ev := Evidence{
		Confidence: 0.85,
		Source:     "import",
		SourceType: "document",
	}
	e := ev.AsEntity()

	// Evidence fields preserved.
	if e.Confidence != ev.Confidence || e.Source != ev.Source || e.SourceType != ev.SourceType {
		t.Fatalf("evidence fields lost: got %+v", e)
	}

	// 16 unrelated fields must be at their zero / nil defaults.
	if e.ID != "" || e.Category != "" || e.Content != "" {
		t.Errorf("Fact fields not zero: id=%q category=%q content=%q", e.ID, e.Category, e.Content)
	}
	if e.Embedding != nil {
		t.Errorf("Embedding not nil: %v", e.Embedding)
	}
	if e.Status != "" {
		t.Errorf("Status not zero: %q", e.Status)
	}
	if e.CreatedAt != nil || e.UpdatedAt != (time.Time{}) || e.LastAccessedAt != nil {
		t.Errorf("timestamp fields not zero/nil: %+v %+v %+v", e.CreatedAt, e.UpdatedAt, e.LastAccessedAt)
	}
	if e.ValidFrom != nil || e.ValidTo != nil {
		t.Errorf("validity window not nil: %+v %+v", e.ValidFrom, e.ValidTo)
	}
	if e.Archived || e.Degree != 0 || e.Priority != 0 {
		t.Errorf("graph/retention fields not zero: archived=%v degree=%d priority=%d", e.Archived, e.Degree, e.Priority)
	}
	if e.ConversationID != "" || e.MessageID != "" || e.ExtractedFrom != "" {
		t.Errorf("provenance fields not zero: %q / %q / %q", e.ConversationID, e.MessageID, e.ExtractedFrom)
	}
}

// TestEvidence_RoundTrip_Exact locks: Evidence → Entity → Evidence is
// a clean round-trip on the 3 evidence fields.
func TestEvidence_RoundTrip_Exact(t *testing.T) {
	original := Evidence{
		Confidence: 0.5,
		Source:     "operator-input",
		SourceType: "manual",
	}
	got := original.AsEntity().AsEvidence()
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("evidence round-trip shifted fields: want %+v, got %+v", original, got)
	}
}

// TestEntityEvidence_RoundTrip_Lossy locks: Entity → Evidence → Entity
// preserves the 3 evidence fields exactly and drops the 16
// unrelated fields. This documents that the upward path (returning
// to Entity from a thin Evidence projection) is deliberately lossy.
func TestEntityEvidence_RoundTrip_Lossy(t *testing.T) {
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
		UpdatedAt:      now,
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
	roundTripped := original.AsEvidence().AsEntity()

	// Evidence fields preserved.
	if roundTripped.Confidence != original.Confidence ||
		roundTripped.Source != original.Source ||
		roundTripped.SourceType != original.SourceType {
		t.Fatalf("evidence fields lost: want %+v, got %+v", original, roundTripped)
	}

	// 16 unrelated fields intentionally dropped.
	if roundTripped.ID != "" || roundTripped.Category != "" || roundTripped.Content != "" {
		t.Errorf("Fact fields not zero: %+v", roundTripped)
	}
	if roundTripped.Status != "" {
		t.Errorf("Status not zero: %q", roundTripped.Status)
	}
	if roundTripped.ConversationID != "" {
		t.Errorf("ConversationID not zero: %q", roundTripped.ConversationID)
	}
}
