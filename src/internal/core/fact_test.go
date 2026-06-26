package core

import (
	"reflect"
	"testing"
	"time"
)

// TestEntityToFact_DropsMetadata locks: Entity → Fact must drop the
// 15 metadata fields so they cannot accidentally leak through the
// "semantic claim" boundary.
func TestEntityToFact_DropsMetadata(t *testing.T) {
	now := time.Now()
	e := Entity{
		ID:             "f-1",
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
	got := e.AsFact()
	want := Fact{
		ID:        "f-1",
		Category:  "world",
		Content:   "Paris is the capital of France",
		Embedding: []float32{0.1, 0.2, 0.3},
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("AsFact(): want %+v, got %+v", want, got)
	}
	// Hard guard: the 4 fact fields are populated, the 15 metadata
	// fields are NOT carried through.
	if got.ID != "f-1" || got.Category != "world" || got.Content == "" || len(got.Embedding) == 0 {
		t.Fatalf("AsFact() lost core fields: %+v", got)
	}
}

// TestFactToEntity_ZerosMetadata locks: Fact → Entity must zero / nil
// out the 15 metadata fields. Callers that need them back must
// supply them from a domain-specific source.
func TestFactToEntity_ZerosMetadata(t *testing.T) {
	f := Fact{
		ID:        "f-2",
		Category:  "opinion",
		Content:   "User prefers Go over Rust",
		Embedding: []float32{0.4, 0.5, 0.6},
	}
	got := f.AsEntity()

	// Core fields preserved.
	if got.ID != f.ID || got.Category != f.Category || got.Content != f.Content {
		t.Fatalf("core fields lost: got %+v", got)
	}
	if !reflect.DeepEqual(f.Embedding, got.Embedding) {
		t.Fatalf("embedding lost: want %+v, got %+v", f.Embedding, got.Embedding)
	}

	// Metadata fields must be at their zero / nil defaults.
	if got.Status != "" {
		t.Errorf("Status not zero: %q", got.Status)
	}
	if got.Confidence != 0 {
		t.Errorf("Confidence not zero: %v", got.Confidence)
	}
	if got.Source != "" || got.SourceType != "" {
		t.Errorf("Source/SourceType not zero: %q / %q", got.Source, got.SourceType)
	}
	if got.CreatedAt != nil || got.UpdatedAt != nil || got.LastAccessedAt != nil {
		t.Errorf("timestamp fields not zero/nil: %+v %+v %+v", got.CreatedAt, got.UpdatedAt, got.LastAccessedAt)
	}
	if got.ValidFrom != nil || got.ValidTo != nil {
		t.Errorf("validity window not nil: %+v %+v", got.ValidFrom, got.ValidTo)
	}
	if got.Archived || got.Degree != 0 || got.Priority != 0 {
		t.Errorf("graph/retention fields not zero: archived=%v degree=%d priority=%d", got.Archived, got.Degree, got.Priority)
	}
	if got.ConversationID != "" || got.MessageID != "" || got.ExtractedFrom != "" {
		t.Errorf("provenance fields not zero: %q / %q / %q", got.ConversationID, got.MessageID, got.ExtractedFrom)
	}
}

// TestFact_RoundTrip_Exact locks: Fact → Entity → Fact is a clean
// round-trip with no mutation of the core 4 fields.
func TestFact_RoundTrip_Exact(t *testing.T) {
	original := Fact{
		ID:        "f-3",
		Category:  "world",
		Content:   "roundtrip",
		Embedding: []float32{0.7, 0.8, 0.9},
	}
	got := original.AsEntity().AsFact()
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("fact round-trip shifted fields: want %+v, got %+v", original, got)
	}
}

// TestEntity_RoundTrip_Lossy locks: Entity → Fact → Entity loses the
// 15 metadata fields but perfectly preserves the core 4. This is the
// documented behavior — the conversion is deliberately lossy on the
// upward path so callers can see what they lose at the boundary.
func TestEntity_RoundTrip_Lossy(t *testing.T) {
	now := time.Now()
	original := Entity{
		ID:             "f-4",
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
	roundTripped := original.AsFact().AsEntity()

	// Core fields preserved.
	if roundTripped.ID != original.ID ||
		roundTripped.Category != original.Category ||
		roundTripped.Content != original.Content {
		t.Fatalf("core fields lost: want %+v, got %+v", original, roundTripped)
	}
	if !reflect.DeepEqual(original.Embedding, roundTripped.Embedding) {
		t.Fatalf("embedding lost: want %+v, got %+v", original.Embedding, roundTripped.Embedding)
	}

	// Metadata fields intentionally lost.
	if roundTripped.Status != "" {
		t.Errorf("Status not zero: %q", roundTripped.Status)
	}
	if roundTripped.Confidence != 0 {
		t.Errorf("Confidence not zero: %v", roundTripped.Confidence)
	}
	if roundTripped.ConversationID != "" {
		t.Errorf("ConversationID not zero: %q", roundTripped.ConversationID)
	}
}
