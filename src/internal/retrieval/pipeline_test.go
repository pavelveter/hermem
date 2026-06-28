package retrieval

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

func TestPipeline_NewPipelineHasDefaults(t *testing.T) {
	p := NewPipeline()
	if p.expand == nil || p.rank == nil || p.assemble == nil || p.render == nil {
		t.Fatal("NewPipeline should initialize all stages")
	}
}

func TestPipeline_DefaultExpandStageName(t *testing.T) {
	s := &defaultExpandStage{}
	if s.Name() != "expand_graph" {
		t.Fatalf("want 'expand_graph', got %q", s.Name())
	}
}

func TestPipeline_DefaultRankStageName(t *testing.T) {
	s := &defaultRankStage{}
	if s.Name() != "score_and_rank" {
		t.Fatalf("want 'score_and_rank', got %q", s.Name())
	}
}

func TestPipeline_DefaultAssemblyStageName(t *testing.T) {
	s := &defaultAssemblyStage{}
	if s.Name() != "bucketize" {
		t.Fatalf("want 'bucketize', got %q", s.Name())
	}
}

func TestPipeline_EmptySeedsReturnsEmpty(t *testing.T) {
	db := openTestDB(t)
	p := NewPipeline()
	result, rendered, err := p.Run(db, nil, core.RetrieveContextOptions{}, "test query")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(result.SeedNodes) != 0 {
		t.Fatal("empty seeds should return empty result")
	}
	if rendered != "" {
		t.Fatal("empty seeds should return empty rendered string")
	}
}

func TestPipeline_WithFixture(t *testing.T) {
	db := openTestDB(t)
	seedEntityWithEmbedding(t, db, "a", "world", "alpha", []float32{1, 0})
	seedEntityWithEmbedding(t, db, "b", "world", "beta", []float32{0, 1})
	db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, 1.0)`, "a", "b")

	p := NewPipeline()
	opts := core.RetrieveContextOptions{
		MaxDepth:       2,
		QueryEmbedding: []float32{1, 0},
	}
	result, rendered, err := p.Run(db, []string{"a"}, opts, "test")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(result.SeedNodes) == 0 {
		t.Fatal("expected at least one seed node")
	}
	if rendered == "" {
		t.Fatal("expected non-empty rendered output")
	}
}

// mockExpandStage is a test double for pipeline stage replacement.
type mockExpandStage struct {
	name string
}

func (m *mockExpandStage) Name() string { return m.name }
func (m *mockExpandStage) Expand(_ context.Context, _ *sql.DB, _ []string, _ core.RetrieveContextOptions, _ int) ([]scannedNode, error) {
	return nil, nil
}

func TestPipeline_SetStages(t *testing.T) {
	p := NewPipeline()
	mock := &mockExpandStage{name: "mock_expand"}
	p.SetExpand(mock)
	if p.expand != mock {
		t.Fatal("SetExpand should replace the expand stage")
	}
}
