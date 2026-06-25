package admin

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

type StatsCollector struct {
	db         *sql.DB
	mu         sync.Mutex
	lastRun    time.Time
	lastResult *Stats
}

func NewStatsCollector(db *sql.DB) *StatsCollector {
	return &StatsCollector{db: db}
}

func (s *StatsCollector) Collect(ctx context.Context) (*Stats, error) {
	s.mu.Lock()
	if time.Since(s.lastRun) < 5*time.Second && s.lastResult != nil {
		c := *s.lastResult
		s.mu.Unlock()
		return &c, nil
	}
	s.mu.Unlock()

	stats := &Stats{CapturedAt: time.Now()}
	var (
		nodeCount, edgeCount, archivedCount,
		contradictionCount, embedCount, pageCount int64
		pageSize int64
	)
	var mu sync.Mutex
	var firstErr error
	setErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	_ = s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize) //nolint:errcheck // PRAGMA read; missing page_size falls back to 4096 below
	if pageSize == 0 {
		pageSize = 4096
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		setErr(s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities").Scan(&nodeCount)) //nolint:errcheck // err propagates via setErr → firstErr
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		setErr(s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM edges").Scan(&edgeCount)) //nolint:errcheck // err propagates via setErr → firstErr
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		setErr(s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities WHERE archived = 1").Scan(&archivedCount)) //nolint:errcheck // err propagates via setErr → firstErr
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		setErr(s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM edges WHERE relation_type = 'contradicts'").Scan(&contradictionCount)) //nolint:errcheck // err propagates via setErr → firstErr
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		setErr(s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities WHERE embedding IS NOT NULL AND length(embedding) > 0").Scan(&embedCount)) //nolint:errcheck // err propagates via setErr → firstErr
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		setErr(s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount)) //nolint:errcheck // err propagates via setErr → firstErr
	}()

	wg.Wait()
	if firstErr != nil {
		return nil, fmt.Errorf("stats collect: %w", firstErr)
	}

	stats.NodeCount = nodeCount
	stats.EdgeCount = edgeCount
	stats.ArchivedCount = archivedCount
	stats.ContradictionCount = contradictionCount
	if nodeCount > 0 {
		stats.EmbeddingCoverage = float64(embedCount) / float64(nodeCount)
	} else {
		stats.EmbeddingCoverage = 1.0
	}
	stats.DBSizeBytes = pageCount * pageSize

	_ = s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'last_gc_run_at'").Scan(&stats.LastGCRunAt) //nolint:errcheck // missing key is fine; LastGCRunAt stays zero
	stats.LastGCArchived = 0

	s.mu.Lock()
	s.lastRun = time.Now()
	s.lastResult = stats
	s.mu.Unlock()

	return stats, nil
}
