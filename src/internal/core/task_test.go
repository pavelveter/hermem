package core

import (
	"reflect"
	"testing"
	"time"
)

// TestEntityToTask_DropsUnrelated locks: Entity → Task must drop
// every field that is not Status / ValidFrom / ValidTo / Priority.
// This is the canonical projection boundary — letting any unrelated
// field bleed through here means callers see task-shape data carrying
// fact content, evidence quality, episode provenance, or graph
// centrality, which would be a category error.
func TestEntityToTask_DropsUnrelated(t *testing.T) {
	now := time.Now()
	later := now.Add(24 * time.Hour)
	e := Entity{
		ID:             "t-1",
		Category:       "task",
		Content:        "Run tests",
		Embedding:      []float32{0.1, 0.2, 0.3},
		Status:         "running",
		Confidence:     0.95,
		Source:         "dialog",
		SourceType:     "extracted",
		CreatedAt:      &now,
		UpdatedAt:      now,
		LastAccessedAt: &now,
		Archived:       false,
		ValidFrom:      &now,
		ValidTo:        &later,
		Degree:         7,
		Priority:       3,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ExtractedFrom:  "msg-1",
	}
	got := e.AsTask()
	want := Task{
		Status:    "running",
		ValidFrom: &now,
		ValidTo:   &later,
		Priority:  3,
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("AsTask(): want %+v, got %+v", want, got)
	}
}

// TestTaskToEntity_ZerosUnrelated locks: Task → Entity must zero /
// nil out every non-task field. Callers that need them back must
// compose with another domain model (Fact, Evidence, Episode).
func TestTaskToEntity_ZerosUnrelated(t *testing.T) {
	now := time.Now()
	later := now.Add(48 * time.Hour)
	tk := Task{
		Status:    "completed",
		ValidFrom: &now,
		ValidTo:   &later,
		Priority:  5,
	}
	e := tk.AsEntity()

	// Task fields preserved (pointer identity must match).
	if e.Status != tk.Status {
		t.Errorf("Status lost: want %q got %q", tk.Status, e.Status)
	}
	if e.ValidFrom != tk.ValidFrom || e.ValidTo != tk.ValidTo {
		t.Errorf("validity window pointer lost: want %p %p, got %p %p", tk.ValidFrom, tk.ValidTo, e.ValidFrom, e.ValidTo)
	}
	if e.Priority != tk.Priority {
		t.Errorf("Priority lost: want %d got %d", tk.Priority, e.Priority)
	}

	// 15 unrelated fields must be at their zero / nil defaults.
	if e.ID != "" || e.Category != "" || e.Content != "" {
		t.Errorf("Fact fields not zero: id=%q category=%q content=%q", e.ID, e.Category, e.Content)
	}
	if e.Embedding != nil {
		t.Errorf("Embedding not nil: %v", e.Embedding)
	}
	if e.Confidence != 0 || e.Source != "" || e.SourceType != "" {
		t.Errorf("Evidence fields not zero: %+v", e)
	}
	if e.CreatedAt != nil || e.UpdatedAt != (time.Time{}) || e.LastAccessedAt != nil {
		t.Errorf("timestamp fields not zero/nil: %+v %+v %+v", e.CreatedAt, e.UpdatedAt, e.LastAccessedAt)
	}
	if e.Archived || e.Degree != 0 {
		t.Errorf("graph fields not zero: archived=%v degree=%d", e.Archived, e.Degree)
	}
	if e.ConversationID != "" || e.MessageID != "" || e.ExtractedFrom != "" {
		t.Errorf("Episode fields not zero: %+v", e)
	}
}

// TestTask_RoundTrip_Exact locks: Task → Entity → Task is a clean
// round-trip on the 4 task fields, including pointer-identity for
// the validity window.
func TestTask_RoundTrip_Exact(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)
	original := Task{
		Status:    "pending",
		ValidFrom: &now,
		ValidTo:   &later,
		Priority:  2,
	}
	got := original.AsEntity().AsTask()
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("task round-trip shifted fields: want %+v, got %+v", original, got)
	}
}

// TestEntityTask_RoundTrip_Lossy locks: Entity → Task → Entity
// preserves the 4 task fields and drops the 15 unrelated fields.
// Documents the deliberately lossy upward path.
func TestEntityTask_RoundTrip_Lossy(t *testing.T) {
	now := time.Now()
	later := now.Add(72 * time.Hour)
	original := Entity{
		ID:             "t-4",
		Category:       "task",
		Content:        "lossy",
		Embedding:      []float32{0.1},
		Status:         "failed",
		Confidence:     0.9,
		Source:         "dialog",
		SourceType:     "extracted",
		CreatedAt:      &now,
		UpdatedAt:      now,
		LastAccessedAt: &now,
		Archived:       true,
		ValidFrom:      &now,
		ValidTo:        &later,
		Degree:         5,
		Priority:       2,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ExtractedFrom:  "msg-1",
	}
	roundTripped := original.AsTask().AsEntity()

	// Task fields preserved.
	if roundTripped.Status != original.Status {
		t.Errorf("Status lost: want %q got %q", original.Status, roundTripped.Status)
	}
	if roundTripped.ValidFrom != original.ValidFrom || roundTripped.ValidTo != original.ValidTo {
		t.Errorf("validity window pointer lost")
	}
	if roundTripped.Priority != original.Priority {
		t.Errorf("Priority lost: want %d got %d", original.Priority, roundTripped.Priority)
	}

	// 15 unrelated fields intentionally dropped.
	if roundTripped.ID != "" || roundTripped.Category != "" || roundTripped.Content != "" {
		t.Errorf("Fact fields not zero: %+v", roundTripped)
	}
	if roundTripped.Confidence != 0 || roundTripped.Source != "" || roundTripped.SourceType != "" {
		t.Errorf("Evidence fields not zero: %+v", roundTripped)
	}
	if roundTripped.ConversationID != "" {
		t.Errorf("Episode field not zero: ConversationID=%q", roundTripped.ConversationID)
	}
}
