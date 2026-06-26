package compression

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

type Compressor struct {
	db        *sql.DB
	extractor core.LLMExtractor
}

func NewCompressor(db *sql.DB, extractor core.LLMExtractor) *Compressor {
	return &Compressor{db: db, extractor: extractor}
}

func (cp *Compressor) Compress(ctx context.Context, entityIDs []string) (*SummaryNode, error) {
	if len(entityIDs) == 0 {
		return nil, fmt.Errorf("compression: Compress: no entity IDs provided")
	}
	entities, err := cp.loadEntities(ctx, entityIDs)
	if err != nil {
		return nil, fmt.Errorf("compression: load entities: %w", err)
	}
	dialog := buildCompressionDialog(entities)
	result, err := cp.extractor.ExtractEntities(ctx, dialog)
	if err != nil {
		return nil, fmt.Errorf("compression: extract: %w", err)
	}

	id := core.NewTaskID()
	summary := &SummaryNode{
		ID:             fmt.Sprintf("summary-%s", id),
		Content:        formatSummary(result),
		CompressedFrom: entityIDs,
		CompressedAt:   time.Now(),
		Confidence:     averageConfidence(entities),
		Provenance:     fmt.Sprintf("compressed from %d entities at %s", len(entityIDs), time.Now().Format(time.RFC3339)),
		Generation:     1,
		ExtractorModel: "llm",
	}
	return summary, nil
}

func (cp *Compressor) CompressCluster(ctx context.Context, clusters [][]string) ([]*SummaryNode, error) {
	if len(clusters) == 0 {
		return nil, nil
	}
	nodes := make([]*SummaryNode, 0, len(clusters))
	for _, cluster := range clusters {
		node, err := cp.Compress(ctx, cluster)
		if err != nil {
			return nil, fmt.Errorf("compression: compress cluster: %w", err)
		}
		nodes = append(nodes, node)
	}
	if nodes == nil {
		return []*SummaryNode{}, nil
	}
	return nodes, nil
}

func (cp *Compressor) loadEntities(ctx context.Context, ids []string) ([]core.Entity, error) {
	phs, args := store.InClauseArgs(ids)
	query := fmt.Sprintf("SELECT id, category, content, confidence FROM entities WHERE id IN (%s)", phs)
	rows, err := cp.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []core.Entity
	for rows.Next() {
		var e core.Entity
		var conf sql.NullFloat64
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &conf); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		if conf.Valid {
			e.Confidence = float32(conf.Float64)
		}
		entities = append(entities, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entities, nil
}

func buildCompressionDialog(entities []core.Entity) string {
	var b strings.Builder
	for _, e := range entities {
		fmt.Fprintf(&b, "[%s] %s\n", e.Category, e.Content)
	}
	return b.String()
}

func formatSummary(result *core.ExtractionResult) string {
	if result == nil || len(result.Entities) == 0 {
		return "(no entities extracted)"
	}
	var b strings.Builder
	for _, ent := range result.Entities {
		fmt.Fprintf(&b, "- [%s] %s\n", ent.Category, ent.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

func averageConfidence(entities []core.Entity) float32 {
	if len(entities) == 0 {
		return 0
	}
	var sum float64
	for _, e := range entities {
		sum += float64(e.Confidence)
	}
	return float32(sum / float64(len(entities)))
}
