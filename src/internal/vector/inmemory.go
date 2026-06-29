package vector

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Compile-time interface assertions.
var _ core.VectorIndex = (*InMemoryVectorIndex)(nil)

// maxSearchN caps the per-call entries returned by Search. Pool slots are
// sized to this ceiling so the underlying []float32 and []int arrays are
// reused across calls with amortised allocation cost. Search calls with n
// above this ceiling fall through to a per-call make([]T, n) — the
// threshold is a hot-path optimisation, not a hard limit.
const maxSearchN = 50_000

// defaultMaxVectors is the default cap for the in-memory vector index.
// When reached, LRU eviction reclaims the least-recently-accessed entry.
const defaultMaxVectors = 500_000

// dotBuf / intBuf wrap fixed-size arrays so the sync.Pool boxes a
// pointer to a stack-sized value instead of pushing a slice header to
// heap on every New call.
type dotBuf struct {
	buf [maxSearchN]float32
}

type intBuf struct {
	buf [maxSearchN]int
}

var (
	dotPool = sync.Pool{
		New: func() any {
			return &dotBuf{}
		},
	}
	intPool = sync.Pool{
		New: func() any {
			return &intBuf{}
		},
	}
)

// getDots / putDots: amortise the cosine-scratch buffer across searches.
// Caller MUST defer putDots(). The pool slot cap is canonical maxSearchN;
// slices with a different cap are dropped on Put so the next Get() always
// sees the same capacity.
func getDots(n int) []float32 {
	if n > maxSearchN {
		return make([]float32, n)
	}
	d := dotPool.Get().(*dotBuf) //nolint:errcheck // sync.Pool invariant
	return d.buf[:n]
}

func putDots(d []float32) {
	if cap(d) != maxSearchN {
		return
	}
	dotPool.Put(&dotBuf{})
}

func getInts(n int) []int {
	if n > maxSearchN {
		return make([]int, n)
	}
	ib := intPool.Get().(*intBuf) //nolint:errcheck // sync.Pool invariant
	return ib.buf[:n]
}

func putInts(d []int) {
	if cap(d) != maxSearchN {
		return
	}
	intPool.Put(&intBuf{})
}

type vectorEntry struct {
	id         string
	vec        []float32
	norm       float32
	lastAccess time.Time
}

// InMemoryVectorIndex holds all vectors in memory with a row-major flatMatrix for batch cosine.
// When maxVectors is set (>0), LRU eviction reclaims the least-recently-accessed
// entry when the cap is reached.
type InMemoryVectorIndex struct {
	db         *sql.DB
	mu         sync.RWMutex
	entries    []vectorEntry
	flatMatrix []float32
	cols       int
	byID       map[string]int
	maxVectors int
}

// NewInMemoryVectorIndex creates an index with default capacity.
func NewInMemoryVectorIndex(db *sql.DB) *InMemoryVectorIndex {
	return NewInMemoryVectorIndexWithCap(db, defaultMaxVectors)
}

// NewInMemoryVectorIndexWithCap creates an index with a configurable
// maximum vector count. When the cap is reached, LRU eviction reclaims
// the least-recently-accessed entry. Set maxVectors <= 0 for unlimited.
func NewInMemoryVectorIndexWithCap(db *sql.DB, maxVectors int) *InMemoryVectorIndex {
	idx := &InMemoryVectorIndex{
		db:         db,
		byID:       make(map[string]int),
		maxVectors: maxVectors,
	}
	idx.load()
	return idx
}

// load pulls every non-archived entity from the DB on startup.
// Vectors are normalized on load so Search can skip per-row norm division.
func (idx *InMemoryVectorIndex) load() {
	start := time.Now()

	// Pre-count rows to right-size slices and avoid repeated reallocations.
	var count int
	if err := idx.db.QueryRow(`SELECT COUNT(*) FROM entities WHERE archived = 0 AND embedding IS NOT NULL`).Scan(&count); err != nil {
		count = 0
	}
	if count > 0 {
		idx.entries = make([]vectorEntry, 0, count)
		// flatMatrix capacity unknown until first row (dim is sticky), but
		// pre-count at least avoids the initial growth from zero.
		idx.flatMatrix = make([]float32, 0, count*3) // rough estimate, will grow
		idx.byID = make(map[string]int, count)
	}

	rows, err := idx.db.Query(`SELECT e.id, e.embedding FROM entities e WHERE e.archived = 0`)
	if err != nil {
		slog.Warn("vector index: load query failed", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			slog.Warn("vector index: load scan failed", "err", err)
			continue
		}
		if emb := store.BytesToEmbedding(blob); emb != nil {
			NormalizeVector(emb)
			if idx.cols == 0 {
				idx.cols = len(emb)
				// Now that dim is known, right-size flatMatrix.
				idx.flatMatrix = make([]float32, 0, count*idx.cols)
			}
			idx.byID[id] = len(idx.entries)
			idx.entries = append(idx.entries, vectorEntry{id: id, vec: emb, norm: 1, lastAccess: time.Now()})
			idx.flatMatrix = append(idx.flatMatrix, emb...)
		}
	}
	slog.Info("vector index loaded", "vectors", len(idx.entries), "dim", idx.cols, "duration", time.Since(start).Round(time.Millisecond))
}

func (idx *InMemoryVectorIndex) Search(_ context.Context, queryEmbedding []float32, limit int) ([]string, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	n := len(idx.entries)
	if n == 0 || idx.cols == 0 {
		return nil, nil
	}
	if len(queryEmbedding) != idx.cols {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrInvalidQueryDim, len(queryEmbedding), idx.cols)
	}
	if len(idx.flatMatrix) != n*idx.cols {
		return nil, fmt.Errorf("%w: matrix has %d, expected %d", ErrMatrixCorrupted, len(idx.flatMatrix), n*idx.cols)
	}
	NormalizeVector(queryEmbedding)
	queryNorm := VectorNorm(queryEmbedding)
	dots := getDots(n)
	defer putDots(dots)
	BatchDotProducts(queryEmbedding, idx.flatMatrix, n, idx.cols, dots)
	for i := range dots {
		dots[i] /= queryNorm
	}
	idxs := getInts(n)
	defer putInts(idxs)
	for i := range idxs {
		idxs[i] = i
	}
	sortByScoreDesc(idxs, dots)
	if limit > n {
		limit = n
	}
	now := time.Now()
	out := make([]string, limit)
	for i := 0; i < limit; i++ {
		e := &idx.entries[idxs[i]]
		e.lastAccess = now
		out[i] = e.id
	}
	return out, nil
}

func (idx *InMemoryVectorIndex) SearchBatch(_ context.Context, queries [][]float32, limit int) ([][]string, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	n := len(idx.entries)
	if n == 0 || idx.cols == 0 {
		out := make([][]string, len(queries))
		return out, nil
	}
	if len(idx.flatMatrix) != n*idx.cols {
		return nil, fmt.Errorf("%w: matrix has %d, expected %d", ErrMatrixCorrupted, len(idx.flatMatrix), n*idx.cols)
	}
	out := make([][]string, len(queries))
	dots := getDots(n)
	defer putDots(dots)
	idxs := getInts(n)
	defer putInts(idxs)
	now := time.Now()
	for qi, q := range queries {
		if len(q) != idx.cols {
			return nil, fmt.Errorf("%w: query %d has %d, want %d", ErrInvalidQueryDim, qi, len(q), idx.cols)
		}
		NormalizeVector(q)
		qNorm := VectorNorm(q)
		BatchDotProducts(q, idx.flatMatrix, n, idx.cols, dots)
		for i := range dots {
			dots[i] /= qNorm
		}
		for i := range idxs {
			idxs[i] = i
		}
		sortByScoreDesc(idxs, dots)
		l := limit
		if l > n {
			l = n
		}
		ids := make([]string, l)
		for i := 0; i < l; i++ {
			e := &idx.entries[idxs[i]]
			e.lastAccess = now
			ids[i] = e.id
		}
		out[qi] = ids
	}
	return out, nil
}

// Store updates or appends the entry; vec is assumed normalized by caller (dim is sticky from load).
// When maxVectors is set and the cap is reached, the least-recently-accessed entry is evicted.
func (idx *InMemoryVectorIndex) Store(_ context.Context, id string, vec []float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	stored := make([]float32, len(vec))
	copy(stored, vec)
	entry := vectorEntry{id: id, vec: stored, norm: 1, lastAccess: time.Now()}
	if i, ok := idx.byID[id]; ok {
		idx.entries[i] = entry
		if idx.cols > 0 {
			copy(idx.flatMatrix[i*idx.cols:(i+1)*idx.cols], stored)
		}
	} else {
		if idx.cols == 0 {
			idx.cols = len(stored)
		}
		// Evict LRU entry if at capacity.
		if idx.maxVectors > 0 && len(idx.entries) >= idx.maxVectors {
			idx.evictLRU()
		}
		idx.byID[id] = len(idx.entries)
		idx.entries = append(idx.entries, entry)
		idx.flatMatrix = append(idx.flatMatrix, stored...)
	}
	return nil
}

func (idx *InMemoryVectorIndex) Remove(_ context.Context, ids []string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	changed := false
	for _, id := range ids {
		if _, ok := idx.byID[id]; ok {
			delete(idx.byID, id)
			changed = true
		}
	}
	if changed {
		idx.compact()
	}
	return nil
}

// evictLRU removes the least-recently-accessed entry and compacts the matrix.
func (idx *InMemoryVectorIndex) evictLRU() {
	if len(idx.entries) == 0 {
		return
	}
	oldest := 0
	for i := 1; i < len(idx.entries); i++ {
		if idx.entries[i].lastAccess.Before(idx.entries[oldest].lastAccess) {
			oldest = i
		}
	}
	delete(idx.byID, idx.entries[oldest].id)
	idx.compact()
}

// compact rebuilds entries and flatMatrix excluding deleted/evicted entries.
func (idx *InMemoryVectorIndex) compact() {
	newEntries := make([]vectorEntry, 0, len(idx.entries))
	newFlat := make([]float32, 0, len(idx.flatMatrix))
	newByID := make(map[string]int, len(idx.byID))
	for _, e := range idx.entries {
		if _, kept := idx.byID[e.id]; !kept {
			continue
		}
		newByID[e.id] = len(newEntries)
		newEntries = append(newEntries, e)
		newFlat = append(newFlat, e.vec...)
	}
	idx.entries = newEntries
	idx.flatMatrix = newFlat
	idx.byID = newByID
}

// sortByScoreDesc sorts idxs in descending order of scores[idxs[i]].
// Insertion sort — n is small enough in practice and the function avoids reflection overhead.
func sortByScoreDesc(idxs []int, scores []float32) {
	for i := 0; i < len(idxs); i++ {
		for j := i + 1; j < len(idxs); j++ {
			if scores[idxs[j]] > scores[idxs[i]] {
				idxs[i], idxs[j] = idxs[j], idxs[i]
			}
		}
	}
}
