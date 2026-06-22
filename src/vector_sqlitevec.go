//go:build sqlite_vec

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func init() {
	sqlite_vec.Auto()
	vectorIndexFactories["sqlite-vec"] = func(db *sql.DB, dim int) VectorIndex {
		return &SqliteVecIndex{db: db, dim: dim}
	}
}

type SqliteVecIndex struct {
	db   *sql.DB
	dim  int
	once sync.Once
}

func (idx *SqliteVecIndex) ensureTables() {
	idx.once.Do(func() {
		idx.db.Exec(fmt.Sprintf(
			`CREATE VIRTUAL TABLE IF NOT EXISTS vec_entities USING vec0(
				embedding FLOAT32[%d],
				entity_id TEXT
			)`, idx.dim))
	})
}

func (idx *SqliteVecIndex) Search(_ context.Context, queryEmbedding []float32, topK int) ([]string, error) {
	idx.ensureTables()

	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}

	queryBytes, err := sqlite_vec.SerializeFloat32(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize query embedding: %w", err)
	}

	rows, err := idx.db.Query(`
		SELECT entity_id
		FROM vec_entities
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT ?
	`, queryBytes, topK)
	if err != nil {
		return nil, fmt.Errorf("failed to query vec_entities: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan vec_entities: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating vec_entities: %w", err)
	}
	return ids, nil
}

func (idx *SqliteVecIndex) SearchBatch(ctx context.Context, queries [][]float32, limit int) ([][]string, error) {
	results := make([][]string, len(queries))
	for i, q := range queries {
		ids, err := idx.Search(ctx, q, limit)
		if err != nil {
			return nil, fmt.Errorf("batch search query %d: %w", i, err)
		}
		results[i] = ids
	}
	return results, nil
}

func (idx *SqliteVecIndex) getRowID(entityID string) (int64, error) {
	return ensureEntityID(idx.db, entityID)
}

func (idx *SqliteVecIndex) Store(_ context.Context, id string, vec []float32) error {
	idx.ensureTables()

	vecBytes, err := sqlite_vec.SerializeFloat32(vec)
	if err != nil {
		return fmt.Errorf("failed to serialize embedding: %w", err)
	}

	rowID, err := idx.getRowID(id)
	if err != nil {
		return fmt.Errorf("failed to get rowid for %q: %w", id, err)
	}

	_, err = idx.db.Exec(`
		INSERT OR REPLACE INTO vec_entities (rowid, embedding, entity_id)
		VALUES (?, ?, ?)
	`, rowID, vecBytes, id)
	return err
}

func (idx *SqliteVecIndex) Remove(ctx context.Context, ids []string) error {
	idx.ensureTables()

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	inClause := strings.Join(placeholders, ",")
	if _, err := idx.db.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM vec_entities WHERE entity_id IN (%s)", inClause), args...); err != nil {
		return fmt.Errorf("remove from vec_entities: %w", err)
	}
	if _, err := idx.db.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM id_map WHERE entity_id IN (%s)", inClause), args...); err != nil {
		return fmt.Errorf("remove from id_map: %w", err)
	}
	return nil
}
