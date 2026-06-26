package core

import (
	"reflect"
	"testing"
	"time"
)

// TestCompose_FullRoundTrip locks: Decompose → Compose is the
// identity operation. Decompose via .AsX() projection methods on
// Entity, then Compose the projections back, and every one of the
// 19 fields must match the original Entity (deep-equal for value
// types and pointer-identity for *time.Time fields).
func TestCompose_FullRoundTrip(t *testing.T) {
	now := time.Now()
	later := now.Add(24 * time.Hour)
	earlier := now.Add(-7 * 24 * time.Hour)
	original := Entity{
		// Fact band.
		ID:        "c-1",
		Category:  "world",
		Content:   "Paris is the capital of France",
		Embedding: []float32{0.1, 0.2, 0.3},
		// Evidence band.
		Confidence: 0.95,
		Source:     "dialog",
		SourceType: "extracted",
		// Episode band.
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ExtractedFrom:  "msg-1",
		// Task band.
		Status:    "completed",
		ValidFrom: &earlier,
		ValidTo:   &later,
		Priority:  3,
		// Belief band.
		CreatedAt:      &earlier,
		UpdatedAt:      &now,
		LastAccessedAt: &later,
		Archived:       false,
		Degree:         7,
	}
	rebuilt := Compose(
		original.AsFact(),
		original.AsEvidence(),
		original.AsEpisode(),
		original.AsTask(),
		original.AsBelief(),
	)
	if !reflect.DeepEqual(original, rebuilt) {
		t.Fatalf("Compose(round-trip) drifted fields: want %+v, got %+v", original, rebuilt)
	}
	// Pointer-identity must be preserved on *time.Time fields.
	if rebuilt.ValidFrom != original.ValidFrom {
		t.Errorf("ValidFrom pointer lost: want %p got %p", original.ValidFrom, rebuilt.ValidFrom)
	}
	if rebuilt.ValidTo != original.ValidTo {
		t.Errorf("ValidTo pointer lost: want %p got %p", original.ValidTo, rebuilt.ValidTo)
	}
	if rebuilt.CreatedAt != original.CreatedAt {
		t.Errorf("CreatedAt pointer lost: want %p got %p", original.CreatedAt, rebuilt.CreatedAt)
	}
	if rebuilt.LastAccessedAt != original.LastAccessedAt {
		t.Errorf("LastAccessedAt pointer lost: want %p got %p", original.LastAccessedAt, rebuilt.LastAccessedAt)
	}
}

// TestCompose_RecoversFromPartial locks: Starting from a zero
// Entity and composing only the pieces a caller has populated
// yields the correct band layout. Each band collapses to zero
// values when its model is empty, but the other bands remain
// intact — partial decomposition is safe.
func TestCompose_RecoversFromPartial(t *testing.T) {
	now := time.Now()
	f := Fact{
		ID:       "p-1",
		Category: "task",
		Content:  "Run tests",
	}
	ev := Evidence{
		Confidence: 0.8,
		Source:     "operator-input",
		SourceType: "manual",
	}
	tk := Task{
		Status:    "in_progress",
		Priority:  2,
		ValidFrom: &now,
	}
	// Episode zero-value, Belief zero-value.
	e := Compose(f, ev, Episode{}, tk, Belief{})

	// Fact band populated.
	if e.ID != "p-1" || e.Category != "task" || e.Content != "Run tests" || e.Embedding != nil {
		t.Errorf("Fact band wrong: %+v", e)
	}
	// Evidence band populated.
	if e.Confidence != 0.8 || e.Source != "operator-input" || e.SourceType != "manual" {
		t.Errorf("Evidence band wrong: %+v", e)
	}
	// Episode band zero-value.
	if e.ConversationID != "" || e.MessageID != "" || e.ExtractedFrom != "" {
		t.Errorf("Episode band not zero: %+v", e)
	}
	// Task band populated (pointer preserved).
	if e.Status != "in_progress" || e.Priority != 2 || e.ValidFrom != tk.ValidFrom || e.ValidTo != nil {
		t.Errorf("Task band wrong: %+v", e)
	}
	// Belief band zero-value (Belief fields zero except UpdatedAt is bare).
	if e.CreatedAt != nil || e.LastAccessedAt != nil || e.Archived || e.Degree != 0 {
		t.Errorf("Belief band not zero: %+v", e)
	}
	if e.UpdatedAt != nil && !e.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt not zero: %v", e.UpdatedAt)
	}
}

// TestCompose_Pure locks: Compose is a deterministic, side-effect-free
// field constructor. Two calls with identical arguments must produce
// identical entities, and the output must exactly reflect the input
// fields. Compose owns no shared mutable state — every output is fully
// determined by its 5 input parameters.
func TestCompose_Pure(t *testing.T) {
	f := Fact{ID: "p-1", Category: "world", Content: "x"}
	ev := Evidence{Confidence: 0.5}
	ep := Episode{ConversationID: "c-7"}
	tk := Task{Status: "pending", Priority: 1}
	b := Belief{Degree: 9}
	a := Compose(f, ev, ep, tk, b)
	c := Compose(f, ev, ep, tk, b)
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("Compose is not deterministic: \n  first:  %+v\n  second: %+v", a, c)
	}
	// Spot-check: every per-model field landed where expected.
	if a.ID != "p-1" {
		t.Errorf("Fact.ID drift: got %q", a.ID)
	}
	if a.Confidence != 0.5 {
		t.Errorf("Evidence.Confidence drift: got %v", a.Confidence)
	}
	if a.ConversationID != "c-7" {
		t.Errorf("Episode.ConversationID drift: got %q", a.ConversationID)
	}
	if a.Priority != 1 {
		t.Errorf("Task.Priority drift: got %d", a.Priority)
	}
	if a.Degree != 9 {
		t.Errorf("Belief.Degree drift: got %d", a.Degree)
	}
}

// TestCompose_GoalBridgesViaTask locks: Goal (item #7) re-views
// Task's 4-field shape but Goal exposes no AsTask() bridge by
// design — the caller field-copies Goal → Task inline and passes
// the resulting Task into Compose. Compose never sees a Goal. The
// compiled Entity must carry all 4 of Goal's lifecycle fields
// correctly (Status / ValidFrom / ValidTo / Priority) and preserve
// *time.Time pointer identity on both window-bound fields through
// the manual bridge.
func TestCompose_GoalBridgesViaTask(t *testing.T) {
	start := time.Now()
	end := start.Add(30 * 24 * time.Hour)
	g := Goal{
		Status:    "in_progress",
		ValidFrom: &start,
		ValidTo:   &end,
		Priority:  4,
	}
	f := Fact{ID: "g-1", Category: "goal", Content: "Ship v0.2.0"}

	// Inline 4-field bridge: Goal's full lifecycle → Task.
	//nolint:staticcheck // by design: Compose never sees a Goal; the test invariant pins field-by-field
	taskFromGoal := Task{
		Status:    g.Status,
		ValidFrom: g.ValidFrom,
		ValidTo:   g.ValidTo,
		Priority:  g.Priority,
	}
	bridged := Compose(f, Evidence{}, Episode{}, taskFromGoal, Belief{})

	// Task band must reflect Goal fields exactly.
	if bridged.Status != "in_progress" {
		t.Errorf("Status drift after Goal→Task bridge: got %q want %q", bridged.Status, g.Status)
	}
	if bridged.ValidTo == nil || *bridged.ValidTo != end {
		t.Errorf("ValidTo drift: got %v want %v", bridged.ValidTo, end)
	}
	if bridged.Priority != 4 {
		t.Errorf("Priority drift: got %d want %d", bridged.Priority, g.Priority)
	}
	// Pointer-identity on both *time.Time fields must survive the bridge.
	if bridged.ValidFrom != g.ValidFrom {
		t.Errorf("ValidFrom pointer lost: want %p got %p", g.ValidFrom, bridged.ValidFrom)
	}
	if bridged.ValidTo != g.ValidTo {
		t.Errorf("ValidTo pointer lost: want %p got %p", g.ValidTo, bridged.ValidTo)
	}
	// Fact band must reflect the goal-marked Entity.
	if bridged.ID != "g-1" || bridged.Category != "goal" || bridged.Content != "Ship v0.2.0" {
		t.Errorf("Fact band wrong after bridge: %+v", bridged)
	}
}
