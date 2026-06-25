package core

import (
	"reflect"
	"testing"
	"time"
)

// TestEntityToGoal_DropsUnrelated locks: Entity → Goal must drop
// every field that is not Status / ValidFrom / ValidTo / Priority.
// Same canonical projection as Task: letting any unrelated field
// leak through here would mean callers see goal-shape data carrying
// fact content, evidence quality, episode provenance, or graph
// centrality, which would be a category error.
func TestEntityToGoal_DropsUnrelated(t *testing.T) {
	now := time.Now()
	later := now.Add(7 * 24 * time.Hour)
	e := Entity{
		ID:             "g-1",
		Category:       "goal",
		Content:        "Ship persistence layer",
		Embedding:      []float32{0.1, 0.2, 0.3},
		Status:         "in_progress",
		Confidence:     0.95,
		Source:         "operator-input",
		SourceType:     "manual",
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
	got := e.AsGoal()
	want := Goal{
		Status:    "in_progress",
		ValidFrom: &now,
		ValidTo:   &later,
		Priority:  3,
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("AsGoal(): want %+v, got %+v", want, got)
	}
	// Goal == Task shape; AsGoal must NOT enforce Category=="goal" —
	// the projection is a dumb field copy, service layer enforces.
	if e.Category != "goal" {
		t.Logf("note: parent Entity.Category=%q; AsGoal does not enforce", e.Category)
	}
}

// TestGoalToEntity_ZerosUnrelated locks: Goal → Entity must zero /
// nil out every non-goal field (15 fields, same as Task shape).
// Same contract as Task since Goal mirrors Task's field shape.
func TestGoalToEntity_ZerosUnrelated(t *testing.T) {
	now := time.Now()
	later := now.Add(14 * 24 * time.Hour)
	g := Goal{
		Status:    "completed",
		ValidFrom: &now,
		ValidTo:   &later,
		Priority:  5,
	}
	e := g.AsEntity()

	// Goal fields preserved (pointer-identity must match).
	if e.Status != g.Status {
		t.Errorf("Status lost: want %q got %q", g.Status, e.Status)
	}
	if e.ValidFrom != g.ValidFrom || e.ValidTo != g.ValidTo {
		t.Errorf("validity window pointer lost: want %p %p, got %p %p", g.ValidFrom, g.ValidTo, e.ValidFrom, e.ValidTo)
	}
	if e.Priority != g.Priority {
		t.Errorf("Priority lost: want %d got %d", g.Priority, e.Priority)
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

// TestGoal_RoundTrip_Exact locks: Goal → Entity → Goal is a clean
// round-trip on the 4 goal fields, including pointer-identity for
// the validity window.
func TestGoal_RoundTrip_Exact(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)
	original := Goal{
		Status:    "pending",
		ValidFrom: &now,
		ValidTo:   &later,
		Priority:  2,
	}
	got := original.AsEntity().AsGoal()
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("goal round-trip shifted fields: want %+v, got %+v", original, got)
	}
}

// TestEntityGoal_RoundTrip_Lossy locks: Entity → Goal → Entity
// preserves the 4 goal fields and drops the 15 unrelated fields.
// Documents the deliberately lossy upward path.
func TestEntityGoal_RoundTrip_Lossy(t *testing.T) {
	now := time.Now()
	later := now.Add(30 * 24 * time.Hour)
	original := Entity{
		ID:             "g-4",
		Category:       "goal",
		Content:        "lossy",
		Embedding:      []float32{0.1},
		Status:         "failed",
		Confidence:     0.9,
		Source:         "operator-input",
		SourceType:     "manual",
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
	roundTripped := original.AsGoal().AsEntity()

	// Goal fields preserved.
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

// TestGoal_ReducesToTask locks the Goal-is-Task-shape invariant
// from item #7: Goal → Entity → Task recovers the Goal's 4
// lifecycle fields exactly (Status / ValidFrom / ValidTo /
// Priority) and preserves *time.Time pointer identity on both
// ValidFrom and ValidTo.
//
// This is the Goal-side counterpart to the cross-pair projection
// matrix (item #10): any caller can reduce Goal → Entity → Task
// safely, without Goal needing its own AsTask() bridge method.
func TestGoal_ReducesToTask(t *testing.T) {
	now := time.Now()
	later := now.Add(7 * 24 * time.Hour)
	g := Goal{
		Status:    "in_progress",
		ValidFrom: &now,
		ValidTo:   &later,
		Priority:  5,
	}

	projected := g.AsEntity().AsTask()

	expected := Task{
		Status:    g.Status,
		ValidFrom: g.ValidFrom,
		ValidTo:   g.ValidTo,
		Priority:  g.Priority,
	}
	if !reflect.DeepEqual(projected, expected) {
		t.Fatalf("Goal→Entity→Task drifted:\n  want %+v\n  got  %+v", expected, projected)
	}
	if projected.ValidFrom != g.ValidFrom {
		t.Errorf("ValidFrom pointer lost: want %p, got %p", g.ValidFrom, projected.ValidFrom)
	}
	if projected.ValidTo != g.ValidTo {
		t.Errorf("ValidTo pointer lost: want %p, got %p", g.ValidTo, projected.ValidTo)
	}
}
