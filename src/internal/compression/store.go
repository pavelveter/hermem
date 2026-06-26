package compression

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pavelveter/hermem/src/internal/store"
)

func loadSummaryNodes(ctx context.Context, db *sql.DB, ids []string) ([]SummaryNode, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	phs, args := store.InClauseArgs(ids)
	query := fmt.Sprintf(
		`SELECT id, content, compressed_from, compressed_at, confidence, provenance, generation, extractor_model, superseded_by, regenerated_at
		 FROM summary_nodes WHERE id IN (%s)`, phs,
	)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []SummaryNode
	for rows.Next() {
		n, err := scanSummaryNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nodes, nil
}

func loadSummaryNode(ctx context.Context, db *sql.DB, id string) (SummaryNode, error) {
	nodes, err := loadSummaryNodes(ctx, db, []string{id})
	if err != nil {
		return SummaryNode{}, err
	}
	if len(nodes) == 0 {
		return SummaryNode{}, fmt.Errorf("summary node not found: %s", id)
	}
	return nodes[0], nil
}

func insertSummaryNode(ctx context.Context, db *sql.DB, n SummaryNode) error {
	fromJSON, err := json.Marshal(n.CompressedFrom)
	if err != nil {
		return fmt.Errorf("marshal compressed_from: %w", err)
	}
	var supersededBy sql.NullString
	if n.SupersededBy != "" {
		supersededBy = sql.NullString{String: n.SupersededBy, Valid: true}
	}
	var regenAt sql.NullTime
	if n.RegeneratedAt != nil {
		regenAt = sql.NullTime{Time: *n.RegeneratedAt, Valid: true}
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO summary_nodes (id, content, compressed_from, compressed_at, confidence, provenance, generation, extractor_model, superseded_by, regenerated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Content, string(fromJSON), n.CompressedAt, float64(n.Confidence),
		n.Provenance, n.Generation, n.ExtractorModel, supersededBy, regenAt,
	)
	if err != nil {
		return fmt.Errorf("insert summary node: %w", err)
	}
	return nil
}

func updateSummaryNodeContent(ctx context.Context, db *sql.DB, id, content string, regeneratedAt time.Time) error {
	_, err := db.ExecContext(ctx,
		`UPDATE summary_nodes SET content = ?, regenerated_at = ? WHERE id = ?`,
		content, regeneratedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update summary node content: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSummaryNode(row scanner) (SummaryNode, error) {
	var n SummaryNode
	var fromJSON string
	var compressedAt time.Time
	var confidence float64
	var supersededBy sql.NullString
	var regenAt sql.NullTime
	if err := row.Scan(&n.ID, &n.Content, &fromJSON, &compressedAt, &confidence, &n.Provenance, &n.Generation, &n.ExtractorModel, &supersededBy, &regenAt); err != nil {
		return SummaryNode{}, err
	}
	n.CompressedAt = compressedAt
	n.Confidence = float32(confidence)
	if supersededBy.Valid {
		n.SupersededBy = supersededBy.String
	}
	if regenAt.Valid {
		n.RegeneratedAt = &regenAt.Time
	}
	if err := json.Unmarshal([]byte(fromJSON), &n.CompressedFrom); err != nil {
		n.CompressedFrom = nil
	}
	return n, nil
}
