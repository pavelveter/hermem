package vector

import (
	"context"
	"database/sql"
	"sort"
	"sync"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/store"
)

// rrfK is the Reciprocal Rank Fusion smoothing constant specified by the
// hybrid search contract.
const rrfK = 60.0

// ftsInit caches which databases have already had EnsureFTS run, avoiding
// repeated DDL on the hot path.
var ftsInit sync.Map

type entityMeta struct {
	Category string
	Content  string
}

// HybridSearchResult is the fused output of the lexical (FTS5) and vector
// channels. Score is the accumulated RRF score (higher is better).
type SearchResult struct {
	ID       string  `json:"id"`
	Category string  `json:"category"`
	Content  string  `json:"content"`
	Score    float64 `json:"score"`
}

// HybridSearch runs the lexical (FTS5) and vector channels in parallel-ish
// sequence, fuses their ranked ID lists with Reciprocal Rank Fusion, and
// hydrates entity metadata from SQLite in a single batched IN query.
//
// Graceful degradation: if FTS5 is unavailable or the lexical query fails,
// or the vector store fails/returns empty, the available channel's results
// are still returned. A nil db or vi disables the corresponding channel.
func HybridSearch(ctx context.Context, db *sql.DB, vi spi.VectorStore, query string, queryEmbedding []float32, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		return nil, nil
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	fetchLimit := limit * 2
	if fetchLimit > maxSearchLimit {
		fetchLimit = maxSearchLimit
	}

	// Ensure the FTS5 virtual table + sync triggers exist (best-effort:
	// silently disabled when the fts5 module is not compiled in). Cached
	// per *sql.DB to avoid repeated DDL on the hot path.
	if db != nil {
		if _, ok := ftsInit.Load(db); !ok {
			EnsureFTS(db)
			ftsInit.Store(db, struct{}{})
		}
	}

	lexicalIDs := lexicalSearch(ctx, db, query, fetchLimit)
	vectorIDs := vectorSearch(ctx, vi, queryEmbedding, fetchLimit)

	// --- RRF fusion ---
	scores := make(map[string]float64)
	for rank, id := range lexicalIDs {
		scores[id] += 1.0 / (rrfK + float64(rank+1))
	}
	for rank, id := range vectorIDs {
		scores[id] += 1.0 / (rrfK + float64(rank+1))
	}
	if len(scores) == 0 {
		return nil, nil
	}

	// --- Batched metadata hydration ---
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	meta := make(map[string]entityMeta, len(ids))
	if db != nil && len(ids) > 0 {
		phs, args := store.InClauseArgs(ids)
		rows, err := db.QueryContext(ctx, `SELECT id, category, content FROM entities WHERE id IN (`+phs+`) AND archived = 0`, args...)
		if err == nil {
			for rows.Next() {
				var id, cat, content string
				if scanErr := rows.Scan(&id, &cat, &content); scanErr == nil {
					meta[id] = entityMeta{Category: cat, Content: content}
				}
			}
			rows.Close()
		}
	}

	// --- Build + sort + truncate ---
	results := make([]SearchResult, 0, len(scores))
	for id, score := range scores {
		m, ok := meta[id]
		if !ok {
			continue
		}
		results = append(results, SearchResult{ID: id, Category: m.Category, Content: m.Content, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].ID < results[j].ID
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func lexicalSearch(ctx context.Context, db *sql.DB, query string, limit int) []string {
	out := make([]string, 0, limit)
	if db == nil || query == "" {
		return out
	}
	rows, err := db.QueryContext(ctx, `SELECT e.id FROM entities_fts f JOIN entities e ON e.id = f.id WHERE entities_fts MATCH ? AND e.archived = 0 ORDER BY rank LIMIT ?`, query, limit)
	if err == nil {
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr == nil {
				out = append(out, id)
			}
		}
		rows.Close()
		return out
	}
	like := "%" + query + "%"
	frows, ferr := db.QueryContext(ctx, `SELECT id FROM entities WHERE archived = 0 AND (id LIKE ? OR category LIKE ? OR content LIKE ?) ORDER BY id LIMIT ?`, like, like, like, limit)
	if ferr == nil {
		for frows.Next() {
			var id string
			if scanErr := frows.Scan(&id); scanErr == nil {
				out = append(out, id)
			}
		}
		frows.Close()
	}
	return out
}

func vectorSearch(ctx context.Context, vi spi.VectorStore, embedding []float32, limit int) []string {
	out := make([]string, 0, limit)
	if vi == nil || len(embedding) == 0 {
		return out
	}
	hits, err := vi.Search(ctx, spi.SearchRequest{Namespace: spi.DefaultNamespace, Vector: embedding, Limit: limit})
	if err != nil {
		return out
	}
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

// EnsureFTS creates the FTS5 virtual table, sync triggers, and backfills
// existing rows. Best-effort: errors are ignored so non-fts5 builds and
// read-only databases degrade gracefully.
func EnsureFTS(db *sql.DB) {
	if db == nil {
		return
	}
	if _, ok := ftsInit.Load(db); ok {
		return
	}
	stmts := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS entities_fts USING fts5(id, category, content)`,
		`CREATE TRIGGER IF NOT EXISTS entities_fts_ai AFTER INSERT ON entities BEGIN INSERT INTO entities_fts(id, category, content) VALUES (new.id, new.category, new.content); END`,
		`CREATE TRIGGER IF NOT EXISTS entities_fts_ad AFTER DELETE ON entities BEGIN DELETE FROM entities_fts WHERE id = old.id; END`,
		`CREATE TRIGGER IF NOT EXISTS entities_fts_au AFTER UPDATE ON entities BEGIN DELETE FROM entities_fts WHERE id = old.id; INSERT INTO entities_fts(id, category, content) VALUES (new.id, new.category, new.content); END`,
	}
	for _, s := range stmts {
		_, _ = db.Exec(s)
	}
	_, _ = db.Exec(`INSERT INTO entities_fts(id, category, content) SELECT e.id, e.category, e.content FROM entities e WHERE e.id NOT IN (SELECT id FROM entities_fts)`)
	ftsInit.Store(db, struct{}{})
}
