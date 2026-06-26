package core_test

import (
	"encoding/json"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// §8 Type-Prep wire-shape pin. Anonymous embed of core.Fact in the 5
// domain slim types (Task / Goal / Episode / Belief / Evidence) promotes
// Fact's JSON tags to top-level keys. These regression tests assert the
// JSON marshal + unmarshal round-trip carries both the embedded Fact
// identity fields AND each slim type's domain-specific fields together
// — catching any future json-tag edit on Fact that would silently shift
// /task/list, /goal/list, /episode, /belief, /evidence response shapes.

func TestTask_JSONWireRoundtripsSlimType(t *testing.T) {
	inp := core.Task{
		Fact:   core.Fact{ID: "t1", Category: "world", Content: "y"},
		Status: "pending",
	}
	data, err := json.Marshal(inp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out core.Task
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != "t1" || out.Category != "world" || out.Content != "y" {
		t.Errorf("identity lost: id=%q category=%q content=%q", out.ID, out.Category, out.Content)
	}
	if out.Status != "pending" {
		t.Errorf("status lost: %q", out.Status)
	}
}

func TestGoal_JSONWireRoundtripsSlimType(t *testing.T) {
	inp := core.Goal{
		Fact:   core.Fact{ID: "g1", Category: "goal", Content: "ship it"},
		Status: "completed",
	}
	data, err := json.Marshal(inp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out core.Goal
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != "g1" || out.Category != "goal" || out.Content != "ship it" {
		t.Errorf("identity lost: id=%q category=%q content=%q", out.ID, out.Category, out.Content)
	}
	if out.Status != "completed" {
		t.Errorf("status lost: %q", out.Status)
	}
}

func TestEpisode_JSONWireRoundtripsSlimType(t *testing.T) {
	inp := core.Episode{
		Fact:           core.Fact{ID: "ep1", Content: "hello"},
		ConversationID: "conv-7",
	}
	data, err := json.Marshal(inp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out core.Episode
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != "ep1" || out.Content != "hello" {
		t.Errorf("identity lost: id=%q content=%q", out.ID, out.Content)
	}
	if out.ConversationID != "conv-7" {
		t.Errorf("provenance lost: %q", out.ConversationID)
	}
}

func TestBelief_JSONWireRoundtripsSlimType(t *testing.T) {
	inp := core.Belief{
		Fact:     core.Fact{ID: "b1", Content: "I think"},
		Degree:   3,
		Archived: false,
	}
	data, err := json.Marshal(inp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out core.Belief
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != "b1" || out.Content != "I think" {
		t.Errorf("identity lost: id=%q content=%q", out.ID, out.Content)
	}
	if out.Degree != 3 {
		t.Errorf("degree lost: %d", out.Degree)
	}
}

func TestEvidence_JSONWireRoundtripsSlimType(t *testing.T) {
	inp := core.Evidence{
		Fact:       core.Fact{ID: "ev1", Content: "source"},
		Confidence: 0.85,
		Source:     "wikipedia",
	}
	data, err := json.Marshal(inp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out core.Evidence
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != "ev1" || out.Content != "source" {
		t.Errorf("identity lost: id=%q content=%q", out.ID, out.Content)
	}
	if out.Confidence != 0.85 || out.Source != "wikipedia" {
		t.Errorf("evidence lost: conf=%f source=%q", out.Confidence, out.Source)
	}
}
