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
	_, err := cp.Compress(context.Background(), nil)
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
	node, err := cp.Compress(context.Background(), []string{"e1"})
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
	node, err := cp.Compress(context.Background(), []string{"e1", "e2"})
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
	nodes, err := cp.CompressCluster(context.Background(), nil)
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
	nodes, err := cp.CompressCluster(context.Background(), [][]string{{"a", "b"}, {"c"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}
