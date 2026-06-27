package compression

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

type mockExtractor struct {
	result *core.ExtractionResult
	err    error
}

func (m *mockExtractor) ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
	return m.result, m.err
}

func TestCompress_NoEntities(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	cp := NewCompressor(db, &mockExtractor{result: &core.ExtractionResult{}, err: nil})
	_, err := cp.Compress(t.Context(), nil)
	if err == nil {
		t.Fatal("expected error for empty IDs")
	}
}

func TestCompress_SingleEntity(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "e1", "world", "Go is a compiled language")
	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "Go is a compiled, statically typed language"},
			},
		},
	})
	node, err := cp.Compress(t.Context(), []string{"e1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.ID == "" {
		t.Fatal("expected non-empty summary ID")
	}
	if node.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", node.Generation)
	}
	if len(node.CompressedFrom) != 1 || node.CompressedFrom[0] != "e1" {
		t.Fatalf("expected CompressedFrom=[e1], got %v", node.CompressedFrom)
	}
}

func TestCompress_MultipleEntities(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "e1", "world", "Go is fast")
	seedEntity(t, db, "e2", "opinion", "Go is elegant")
	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "Go is a fast compiled language"},
				{Category: "opinion", Content: "Go syntax is clean and minimal"},
			},
		},
	})
	node, err := cp.Compress(t.Context(), []string{"e1", "e2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(node.CompressedFrom) != 2 {
		t.Fatalf("expected 2 compressed_from IDs, got %d", len(node.CompressedFrom))
	}
}

func TestCompressCluster_Empty(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	cp := NewCompressor(db, &mockExtractor{result: &core.ExtractionResult{}, err: nil})
	nodes, err := cp.CompressCluster(t.Context(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestCompressCluster_MultipleClusters(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "A is a")
	seedEntity(t, db, "b", "world", "B is b")
	seedEntity(t, db, "c", "world", "C is c")

	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "summary"},
			},
		},
	})
	nodes, err := cp.CompressCluster(t.Context(), [][]string{{"a", "b"}, {"c"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestRecompress_Success(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "e1", "world", "Go is compiled")
	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "Go is a compiled language"},
			},
		},
	})
	first, err := cp.Compress(t.Context(), []string{"e1"})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	recompressed, err := cp.Recompress(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("recompress: %v", err)
	}
	if recompressed.Generation != 2 {
		t.Fatalf("expected generation 2, got %d", recompressed.Generation)
	}
	if recompressed.SupersededBy != "" {
		t.Fatalf("new node should not have superseded_by set")
	}

	loaded, err := loadSummaryNode(t.Context(), db, first.ID)
	if err != nil {
		t.Fatalf("load original: %v", err)
	}
	if loaded.SupersededBy != recompressed.ID {
		t.Fatalf("expected original.superseded_by = %s, got %s", recompressed.ID, loaded.SupersededBy)
	}

	hasOld := false
	for _, id := range recompressed.CompressedFrom {
		if id == first.ID {
			hasOld = true
			break
		}
	}
	if !hasOld {
		t.Fatal("recompressed should include old summary ID in CompressedFrom")
	}
}

func TestRecompress_MaxDepth(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "e1", "world", "data")
	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "summary"},
			},
		},
	})

	node := &SummaryNode{
		ID:             "summary-maxed",
		Content:        "maxed out",
		CompressedFrom: []string{"e1"},
		CompressedAt:   &zeroTime,
		Generation:     MaxRecursionDepth,
		ExtractorModel: "llm",
	}
	if err := insertSummaryNode(t.Context(), db, *node); err != nil {
		t.Fatalf("seed max-depth node: %v", err)
	}

	_, err := cp.Recompress(t.Context(), "summary-maxed")
	if err == nil {
		t.Fatal("expected error for max depth reached")
	}
}

func TestRegenerate_Success(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "e1", "world", "Go is compiled")
	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "regenerated summary"},
			},
		},
	})
	first, err := cp.Compress(t.Context(), []string{"e1"})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	regenerated, err := cp.Regenerate(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if regenerated.ID != first.ID {
		t.Fatalf("expected same ID %s, got %s", first.ID, regenerated.ID)
	}
	if regenerated.Generation != 1 {
		t.Fatalf("expected generation 1 (same gen), got %d", regenerated.Generation)
	}
	if regenerated.RegeneratedAt == nil {
		t.Fatal("expected regenerated_at to be set")
	}
}

func TestProvenance_SurvivesRecompress(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "e1", "world", "Go fast")
	seedEntity(t, db, "e2", "world", "Go simple")
	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "merged summary"},
			},
		},
	})
	first, err := cp.Compress(t.Context(), []string{"e1", "e2"})
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	recompressed, err := cp.Recompress(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("recompress: %v", err)
	}

	hasE1, hasE2, hasOld := false, false, false
	for _, id := range recompressed.CompressedFrom {
		switch id {
		case "e1":
			hasE1 = true
		case "e2":
			hasE2 = true
		case first.ID:
			hasOld = true
		}
	}
	if !hasE1 || !hasE2 {
		t.Fatal("original entity IDs should survive recompression")
	}
	if !hasOld {
		t.Fatal("old summary ID should be carried forward")
	}
}
