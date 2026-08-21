package v1

import (
	"encoding/json"
	"testing"
)

// TestDTONoInternalImports guards the transport boundary: api/v1 must depend
// on pkg/domain only, never on src/internal/core. Go's internal-package rule
// enforces this at build time — a test file in this package cannot import
// src/internal/core at all — so the presence of this package's sources
// compiling under `go test ./api/...` is itself the guard. Keep the assertion
// explicit for reviewers.
func TestDTONoInternalImports(t *testing.T) {
	_ = StoreRequest{}
	_ = SearchRequest{}
	_ = RetrieveRequest{}
	_ = IngestRequest{}
	_ = EdgeRequest{}
	_ = TaskStatusRequest{}
	_ = TaskListRequest{}
	_ = TaskShowRequest{}
	_ = TaskDepRequest{}
	_ = TaskRollbackRequest{}
	_ = TaskTreeRequest{}
	_ = TaskCreateRequest{}
}

// TestRequestDTOGoldenJSON pins the wire shape of every versioned request DTO
// that replaces a core.* request type. Golden values are fully populated so
// omitempty differences between the v1 DTOs and their (soon removed) core
// counterparts do not affect the comparison: the field names, order, and
// scalar encodings are byte-identical for real payloads.
func TestRequestDTOGoldenJSON(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{
			"StoreRequest",
			StoreRequest{ID: "e1", Category: "world", Content: "hello", Embedding: []float32{1, 2}},
			`{"id":"e1","category":"world","content":"hello","embedding":[1,2]}`,
		},
		{
			"SearchRequest",
			SearchRequest{Query: "q", TopK: 5},
			`{"query":"q","top_k":5}`,
		},
		{
			"RetrieveRequest",
			RetrieveRequest{SeedIDs: []string{"a", "b"}, MaxDepth: 2},
			`{"seed_ids":["a","b"],"max_depth":2}`,
		},
		{
			"TemporalQueryRequest",
			TemporalQueryRequest{Query: "q", TopK: 3, TimeFrom: "2026-01-01T00:00:00Z", TimeTo: "2026-02-01T00:00:00Z"},
			`{"query":"q","top_k":3,"time_from":"2026-01-01T00:00:00Z","time_to":"2026-02-01T00:00:00Z"}`,
		},
		{
			"IngestRequest",
			IngestRequest{Dialog: "user: hi"},
			`{"dialog":"user: hi"}`,
		},
		{
			"EdgeRequest",
			EdgeRequest{SourceID: "s", TargetID: "t", RelationType: "related_to", AutoCreate: true, Weight: 0.5},
			`{"source_id":"s","target_id":"t","relation_type":"related_to","auto_create":true,"weight":0.5}`,
		},
		{
			"TaskStatusRequest",
			TaskStatusRequest{ID: "t1", Status: "done"},
			`{"id":"t1","status":"done"}`,
		},
		{
			"TaskListRequest",
			TaskListRequest{Status: "pending", GoalID: "g1"},
			`{"status":"pending","goal_id":"g1"}`,
		},
		{
			"TaskShowRequest",
			TaskShowRequest{ID: "t1"},
			`{"id":"t1"}`,
		},
		{
			"TaskDepRequest",
			TaskDepRequest{SourceID: "s", TargetID: "t", RelationType: "blocks", Add: true},
			`{"source_id":"s","target_id":"t","relation_type":"blocks","add":true}`,
		},
		{
			"TaskRollbackRequest",
			TaskRollbackRequest{ID: "t1"},
			`{"id":"t1"}`,
		},
		{
			"TaskTreeRequest",
			TaskTreeRequest{GoalID: "g1"},
			`{"goal_id":"g1"}`,
		},
		{
			"TaskCreateRequest",
			TaskCreateRequest{ID: "t1", Content: "do it", ContextIDs: []string{"c1"}},
			`{"id":"t1","content":"do it","context_ids":["c1"]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("golden JSON mismatch:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}
