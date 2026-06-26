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

const (
	MaxRecursionDepth = 3
)

type Compressor struct {
	db        *sql.DB
	extractor core.LLMExtractor
	metrics   *Metrics
}

func NewCompressor(db *sql.DB, extractor core.LLMExtractor) *Compressor {
	return &Compressor{db: db, extractor: extractor}
}

func (cp *Compressor) WithMetrics(m *Metrics) *Compressor {
	cp.metrics = m
	return cp
}

func (cp *Compressor) Compress(ctx context.Context, entityIDs []string) (*SummaryNode, error) {
	start := time.Now()
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
	if err := insertSummaryNode(ctx, cp.db, *summary); err != nil {
		return nil, fmt.Errorf("compression: persist summary: %w", err)
	}
	if cp.metrics != nil {
		cp.metrics.IncCompress()
		cp.metrics.AddCompressedEntities(len(entityIDs))
		cp.metrics.ObserveCompressDuration(time.Since(start))
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
		if cp.metrics != nil {
			cp.metrics.AddClusterSize(len(cluster))
		}
		nodes = append(nodes, node)
	}
	return core.NormalizeSlice(nodes), nil
}

func (cp *Compressor) Recompress(ctx context.Context, summaryID string) (*SummaryNode, error) {
	start := time.Now()
	existing, err := loadSummaryNode(ctx, cp.db, summaryID)
	if err != nil {
		return nil, fmt.Errorf("compression: recompress load: %w", err)
	}
	if existing.Generation >= MaxRecursionDepth {
		return nil, fmt.Errorf("compression: max recursion depth (%d) reached for %s", MaxRecursionDepth, summaryID)
	}

	sourceIDs := make([]string, len(existing.CompressedFrom))
	copy(sourceIDs, existing.CompressedFrom)
	sourceIDs = append(sourceIDs, summaryID)

	entities, err := cp.loadEntities(ctx, sourceIDs)
	if err != nil {
		return nil, fmt.Errorf("compression: recompress load entities: %w", err)
	}
	dialog := buildCompressionDialog(entities)
	result, err := cp.extractor.ExtractEntities(ctx, dialog)
	if err != nil {
		return nil, fmt.Errorf("compression: recompress extract: %w", err)
	}

	id := core.NewTaskID()
	provenance := fmt.Sprintf("recompressed from %s (gen %d) + %d entities at %s",
		summaryID, existing.Generation, len(existing.CompressedFrom), time.Now().Format(time.RFC3339))

	newNode := &SummaryNode{
		ID:             fmt.Sprintf("summary-%s", id),
		Content:        formatSummary(result),
		CompressedFrom: sourceIDs,
		CompressedAt:   time.Now(),
		Confidence:     existing.Confidence,
		Provenance:     provenance,
		Generation:     existing.Generation + 1,
		ExtractorModel: "llm",
	}
	if err := insertSummaryNode(ctx, cp.db, *newNode); err != nil {
		return nil, fmt.Errorf("compression: persist recompressed: %w", err)
	}

	if err := markSuperseded(ctx, cp.db, summaryID, newNode.ID); err != nil {
		return nil, fmt.Errorf("compression: mark superseded: %w", err)
	}
	if cp.metrics != nil {
		cp.metrics.IncRecompress()
		cp.metrics.AddCompressedEntities(len(sourceIDs))
		cp.metrics.ObserveRecompressDuration(time.Since(start))
	}
	return newNode, nil
}

func (cp *Compressor) Regenerate(ctx context.Context, summaryID string) (*SummaryNode, error) {
	start := time.Now()
	existing, err := loadSummaryNode(ctx, cp.db, summaryID)
	if err != nil {
		return nil, fmt.Errorf("compression: regenerate load: %w", err)
	}

	entities, err := cp.loadEntities(ctx, existing.CompressedFrom)
	if err != nil {
		return nil, fmt.Errorf("compression: regenerate load entities: %w", err)
	}
	dialog := buildCompressionDialog(entities)
	result, err := cp.extractor.ExtractEntities(ctx, dialog)
	if err != nil {
		return nil, fmt.Errorf("compression: regenerate extract: %w", err)
	}

	newContent := formatSummary(result)
	now := time.Now()
	if err := updateSummaryNodeContent(ctx, cp.db, summaryID, newContent, now); err != nil {
		return nil, fmt.Errorf("compression: regenerate persist: %w", err)
	}

	existing.Content = newContent
	existing.RegeneratedAt = &now
	existing.CompressedAt = time.Now()
	if cp.metrics != nil {
		cp.metrics.IncRegenerate()
		cp.metrics.ObserveCompressDuration(time.Since(start))
	}
	return &existing, nil
}

func markSuperseded(ctx context.Context, db *sql.DB, oldID, newID string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE summary_nodes SET superseded_by = ? WHERE id = ?`,
		newID, oldID,
	)
	if err != nil {
		return fmt.Errorf("mark superseded %s -> %s: %w", oldID, newID, err)
	}
	return nil
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
