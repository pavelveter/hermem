package compression

import (
	"context"
	"fmt"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

type countingExtractor struct {
	calls  int
	result *core.ExtractionResult
}

func (c *countingExtractor) ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
	c.calls++
	return c.result, nil
}

func TestCompressionIntegration(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("e-%d", i)
		emb := make([]float32, 8)
		emb[0] = float32(i) / 10
		emb[1] = 1.0 - float32(i)/10
		seedEntityFull(t, db, id, "world", fmt.Sprintf("entity %d content", i), "", zeroTime, emb)
	}

	extractor := &countingExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "clustered summary"},
			},
		},
	}

	metrics := NewMetrics()
	cp := NewCompressor(db, extractor).WithMetrics(metrics)

	ids := make([]string, 10)
	for i := range ids {
		ids[i] = fmt.Sprintf("e-%d", i)
	}

	clustererCfg := DefaultClustererConfig()
	clustererCfg.SimilarityThreshold = 0.01
	clusterer := NewClusterer(db, clustererCfg)
	clusters, err := clusterer.Cluster(t.Context(), ids)
	if err != nil {
		t.Fatalf("cluster: %v", err)
	}
	if len(clusters) == 0 {
		t.Fatal("expected at least 1 cluster")
	}

	nodes, err := cp.CompressCluster(t.Context(), clusters)
	if err != nil {
		t.Fatalf("compress cluster: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected at least 1 summary node")
	}

	first := nodes[0]
	if first.ID == "" {
		t.Fatal("expected non-empty summary ID")
	}
	if first.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", first.Generation)
	}
	if len(first.CompressedFrom) < 2 {
		t.Fatalf("expected at least 2 compressed_from IDs, got %d", len(first.CompressedFrom))
	}

	recompressed, err := cp.Recompress(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("recompress: %v", err)
	}
	if recompressed.Generation != 2 {
		t.Fatalf("expected generation 2 after recompress, got %d", recompressed.Generation)
	}

	loadedOrig, err := loadSummaryNode(t.Context(), db, first.ID)
	if err != nil {
		t.Fatalf("load original: %v", err)
	}
	if loadedOrig.SupersededBy != recompressed.ID {
		t.Fatalf("expected original.superseded_by = %s, got %s", recompressed.ID, loadedOrig.SupersededBy)
	}

	extractor2 := &countingExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "regenerated content"},
			},
		},
	}
	cp2 := NewCompressor(db, extractor2).WithMetrics(metrics)
	regenerated, err := cp2.Regenerate(t.Context(), first.ID)
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

	if metrics.CompressCount() == 0 {
		t.Fatal("expected compress count > 0")
	}
	if metrics.RecompressCount() == 0 {
		t.Fatal("expected recompress count > 0")
	}
	if metrics.RegenerateCount() == 0 {
		t.Fatal("expected regenerate count > 0")
	}
	if metrics.CompressedEntities() == 0 {
		t.Fatal("expected compressed entities count > 0")
	}
	if metrics.ClusterCount() == 0 {
		t.Fatal("expected cluster count > 0")
	}
}
