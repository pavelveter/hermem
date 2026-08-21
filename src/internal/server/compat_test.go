package server

import (
	"encoding/json"
	"testing"

	apiv1 "github.com/pavelveter/hermem/api/v1"
	"github.com/pavelveter/hermem/src/internal/core"
)

// TestAPIV1WireCompatCoreRequestTypes pins byte-level transport compatibility
// between the versioned api/v1 DTOs and the core.* request/response types they
// replace during the breaking-release migration. Each case marshals the same
// logical payload through both types and requires identical JSON bytes.
//
// Values are fully populated so omitempty divergences (e.g. api/v1 adds
// omitempty to TopK/MaxDepth/AutoCreate) do not surface: for real payloads the
// field names, order, and scalar encodings are identical. This test must live
// under src/ because api/v1 is a public package and cannot import core.
func TestAPIV1WireCompatCoreRequestTypes(t *testing.T) {
	cases := []struct {
		name string
		v1   any
		core any
	}{
		{"StoreRequest",
			apiv1.StoreRequest{ID: "e1", Category: "world", Content: "hello", Embedding: []float32{1, 2}},
			core.StoreRequest{ID: "e1", Category: "world", Content: "hello", Embedding: []float32{1, 2}}},
		{"SearchRequest",
			apiv1.SearchRequest{Query: "q", TopK: 5},
			core.SearchRequest{Query: "q", TopK: 5}},
		{"RetrieveRequest",
			apiv1.RetrieveRequest{SeedIDs: []string{"a", "b"}, MaxDepth: 2},
			core.RetrieveRequest{SeedIDs: []string{"a", "b"}, MaxDepth: 2}},
		{"TemporalQueryRequest",
			apiv1.TemporalQueryRequest{Query: "q", TopK: 3, TimeFrom: "2026-01-01T00:00:00Z", TimeTo: "2026-02-01T00:00:00Z"},
			core.TemporalQueryRequest{Query: "q", TopK: 3, TimeFrom: "2026-01-01T00:00:00Z", TimeTo: "2026-02-01T00:00:00Z"}},
		{"IngestRequest",
			apiv1.IngestRequest{Dialog: "user: hi"},
			core.IngestRequest{Dialog: "user: hi"}},
		{"EdgeRequest",
			apiv1.EdgeRequest{SourceID: "s", TargetID: "t", RelationType: "related_to", AutoCreate: true, Weight: 0.5},
			core.EdgeRequest{SourceID: "s", TargetID: "t", RelationType: "related_to", AutoCreate: true, Weight: 0.5}},
		{"TaskStatusRequest",
			apiv1.TaskStatusRequest{ID: "t1", Status: "done"},
			core.TaskStatusRequest{ID: "t1", Status: "done"}},
		{"TaskListRequest",
			apiv1.TaskListRequest{Status: "pending", GoalID: "g1"},
			core.TaskListRequest{Status: "pending", GoalID: "g1"}},
		{"TaskShowRequest",
			apiv1.TaskShowRequest{ID: "t1"},
			core.TaskShowRequest{ID: "t1"}},
		{"TaskDepRequest",
			apiv1.TaskDepRequest{SourceID: "s", TargetID: "t", RelationType: "blocks", Add: true},
			core.TaskDepRequest{SourceID: "s", TargetID: "t", RelationType: "blocks", Add: true}},
		{"TaskRollbackRequest",
			apiv1.TaskRollbackRequest{ID: "t1"},
			core.TaskRollbackRequest{ID: "t1"}},
		{"TaskTreeRequest",
			apiv1.TaskTreeRequest{GoalID: "g1"},
			core.TaskTreeRequest{GoalID: "g1"}},
		{"TaskCreateRequest",
			apiv1.TaskCreateRequest{ID: "t1", Content: "do it", ContextIDs: []string{"c1"}},
			core.TaskCreateRequest{ID: "t1", Content: "do it", ContextIDs: []string{"c1"}}},
		{"ErrorResponse",
			apiv1.ErrorResponse{Error: "bad", Code: "invalid_input", Field: "id"},
			core.ErrorResponse{Error: "bad", Code: "invalid_input", Field: "id"}},
		{"TaskRollbackResponse",
			apiv1.TaskRollbackResponse{RollbackTaskID: "t2"},
			core.TaskRollbackResponse{RollbackTaskID: "t2"}},
		{"TaskTreeResponse",
			apiv1.TaskTreeResponse{Tree: "root"},
			core.TaskTreeResponse{Tree: "root"}},
		{"TaskCreateResponse",
			apiv1.TaskCreateResponse{ID: "t1", Status: "pending"},
			core.TaskCreateResponse{ID: "t1", Status: "pending"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v1b, err := json.Marshal(tc.v1)
			if err != nil {
				t.Fatalf("marshal v1: %v", err)
			}
			coreb, err := json.Marshal(tc.core)
			if err != nil {
				t.Fatalf("marshal core: %v", err)
			}
			if string(v1b) != string(coreb) {
				t.Fatalf("wire incompatibility:\n v1:   %s\n core: %s", v1b, coreb)
			}
		})
	}
}
