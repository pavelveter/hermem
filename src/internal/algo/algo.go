// Package algo provides analytical algorithms: GC, re-embedding, verification, caching.
package algo

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// GarbageCollector periodically archives stale observation nodes.
func GarbageCollector(ctx context.Context, db *sql.DB, vi core.VectorIndex, policy core.RetentionPolicy) {
	ticker := time.NewTicker(policy.RunInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-policy.ObservationTTL)
			rows, err := db.QueryContext(ctx, `SELECT id FROM entities WHERE category = 'observation' AND updated_at < ? AND archived = 0 LIMIT ?`, cutoff, policy.DeleteBatchSize)
			if err != nil {
				slog.Error("gc query", "err", err)
				continue
			}
			var ids []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				ids = append(ids, id)
			}
			rows.Close()
			if len(ids) == 0 {
				continue
			}
			tx, _ := db.BeginTx(ctx, nil)
			for _, id := range ids {
				tx.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, id)
			}
			tx.Commit()
			vi.Remove(ctx, ids)
			slog.Info("gc archived", "count", len(ids))
		}
	}
}

// NeedsReEmbed checks if dimension drift requires re-embedding.
func NeedsReEmbed(db *sql.DB, configuredDim int) (needs bool, oldDim int, err error) {
	err = db.QueryRow("SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&oldDim)
	if err == sql.ErrNoRows {
		return false, configuredDim, nil
	}
	if err != nil {
		return false, 0, err
	}
	return oldDim != configuredDim, oldDim, nil
}

// embedWork is a work item for ReEmbedAll.
type embedWork struct {
	id, content string
}

// ReEmbedAll re-embeds all entities with the current embedder.
func ReEmbedAll(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, configuredDim int, batchSize int, modelName string) (core.ReEmbedResult, error) {
	start := time.Now()
	result := core.ReEmbedResult{NewDim: configuredDim}

	oldDim := configuredDim
	if err := db.QueryRow("SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&oldDim); err != nil && err != sql.ErrNoRows {
		return result, fmt.Errorf("read old dim: %w", err)
	}
	result.OldDim = oldDim

	if modelName != "" {
		db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('model_name', ?)", modelName)
	}

	rows, err := db.QueryContext(ctx, `SELECT id, content FROM entities WHERE archived = 0 ORDER BY id`)
	if err != nil {
		return result, fmt.Errorf("query entities: %w", err)
	}
	defer rows.Close()

	var batch []embedWork
	for rows.Next() {
		var w embedWork
		if err := rows.Scan(&w.id, &w.content); err != nil {
			return result, fmt.Errorf("scan: %w", err)
		}
		result.TotalEntities++
		if w.content == "" {
			result.Skipped++
			continue
		}
		batch = append(batch, w)
		if len(batch) >= batchSize {
			if err := processBatch(ctx, db, vi, embedder, batch, configuredDim, &result); err != nil {
				return result, err
			}
			batch = nil
		}
	}
	if len(batch) > 0 {
		if err := processBatch(ctx, db, vi, embedder, batch, configuredDim, &result); err != nil {
			return result, err
		}
	}

	db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('embedding_dim', ?)", fmt.Sprintf("%d", configuredDim))
	result.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return result, nil
}

func processBatch(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, items []embedWork, dim int, result *core.ReEmbedResult) error {
	result.Batches++
	for _, item := range items {
		emb, err := embedder.Embed(ctx, item.content)
		if err != nil {
			result.Failed++
			slog.Warn("re-embed failed", "id", item.id, "err", err)
			continue
		}
		if len(emb) != dim {
			result.Failed++
			slog.Warn("re-embed dim mismatch", "id", item.id, "got", len(emb), "want", dim)
			continue
		}
		vector.NormalizeVector(emb)
		blob := store.EmbeddingToBytes(emb)
		if _, err := db.ExecContext(ctx, `UPDATE entities SET embedding = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, blob, item.id); err != nil {
			result.Failed++
			slog.Warn("re-embed update", "id", item.id, "err", err)
			continue
		}
		vi.Store(ctx, item.id, emb)
		result.ReEmbedded++
	}
	return nil
}

// VerifyGraph runs read-only integrity checks.
func VerifyGraph(db *sql.DB, schema core.SchemaConfig, vectorDim int) (core.VerifyReport, error) {
	var report core.VerifyReport
	// Check orphan edges.
	rows, err := db.Query(`SELECT ed.source_id, ed.target_id, ed.relation_type FROM edges ed LEFT JOIN entities e1 ON ed.source_id = e1.id LEFT JOIN entities e2 ON ed.target_id = e2.id WHERE e1.id IS NULL OR e2.id IS NULL`)
	if err != nil {
		return report, fmt.Errorf("verify orphan edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var src, dst, rel string
		rows.Scan(&src, &dst, &rel)
		report.Issues = append(report.Issues, fmt.Sprintf("orphan edge: %s -[%s]-> %s", src, rel, dst))
	}
	// Check embedding dimension.
	embRows, err := db.Query(`SELECT id, length(embedding) FROM entities WHERE archived = 0 AND embedding IS NOT NULL AND length(embedding) != ?`, vectorDim*4)
	if err != nil {
		return report, fmt.Errorf("verify dim: %w", err)
	}
	defer embRows.Close()
	for embRows.Next() {
		var id string
		var l int
		embRows.Scan(&id, &l)
		report.Issues = append(report.Issues, fmt.Sprintf("dimension mismatch: %s has %d bytes (want %d)", id, l, vectorDim*4))
	}
	return report, nil
}

// EmbeddingCache provides an LRU cache for embeddings.
type EmbeddingCache struct {
	mu       sync.RWMutex
	capacity int
	entries  map[string]*cacheEntry
	head     *cacheEntry
	tail     *cacheEntry
}

type cacheEntry struct {
	key   string
	value []float32
	prev  *cacheEntry
	next  *cacheEntry
}

func NewEmbeddingCache(capacity int) *EmbeddingCache {
	return &EmbeddingCache{capacity: capacity, entries: make(map[string]*cacheEntry)}
}

func (c *EmbeddingCache) Get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	v := make([]float32, len(e.value))
	copy(v, e.value)
	return v, true
}

func (c *EmbeddingCache) Set(key string, value []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		e.value = append(e.value[:0], value...)
		c.moveToFront(e)
		return
	}
	e := &cacheEntry{key: key, value: append([]float32{}, value...)}
	c.entries[key] = e
	if c.head == nil {
		c.head = e
		c.tail = e
	} else {
		e.next = c.head
		c.head.prev = e
		c.head = e
	}
	for len(c.entries) > c.capacity {
		c.evict()
	}
}

func (c *EmbeddingCache) moveToFront(e *cacheEntry) {
	if c.head == e {
		return
	}
	if e.prev != nil {
		e.prev.next = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev = nil
	e.next = c.head
	c.head.prev = e
	c.head = e
}

func (c *EmbeddingCache) evict() {
	if c.tail == nil {
		return
	}
	delete(c.entries, c.tail.key)
	if c.tail.prev != nil {
		c.tail.prev.next = nil
	}
	c.tail = c.tail.prev
}

// IsContradiction checks if two texts are contradictory via simple heuristics.
func IsContradiction(a, b string) bool {
	negWords := []string{"not", "don't", "doesn't", "isn't", "aren't", "won't", "can't", "never", "no", "hate", "dislike"}
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	for _, n := range negWords {
		if strings.Contains(al, n) != strings.Contains(bl, n) {
			return true
		}
	}
	return false
}

// AgentLoop executes tasks in a goal's dependency tree in topological order.
func AgentLoop(ctx context.Context, db *sql.DB, schema core.SchemaConfig, goalID string, execFunc func(context.Context, core.Entity) error) error {
	for {
		tasks, err := retrieval.GetExecutableTasks(db, schema, goalID)
		if err != nil {
			return fmt.Errorf("agent loop: get executable: %w", err)
		}
		if len(tasks) == 0 {
			break
		}
		for _, task := range tasks {
			if err := execFunc(ctx, task); err != nil {
				return fmt.Errorf("agent loop: exec %s: %w", task.ID, err)
			}
			if err := store.SetStatus(db, schema, task.ID, schema.StateUnblocking); err != nil {
				return fmt.Errorf("agent loop: set status %s: %w", task.ID, err)
			}
		}
	}
	return nil
}

// ExecutionPlan returns tasks for a goal.
func ExecutionPlan(db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	return retrieval.GetExecutableTasks(db, schema, goalID)
}
