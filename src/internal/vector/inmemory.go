package vector

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/pavelveter/hermem/src/internal/store"
)

// maxSearchN caps the per-call entries returned by Search. Pool slots are
// sized to this ceiling so the underlying []float32 and []int arrays are
// reused across calls with amortised allocation cost. Search calls with n
// above this ceiling fall through to a per-call make([]T, n) — the
// threshold is a hot-path optimisation, not a hard limit.
const maxSearchN = 50_000

var (
	dotPool = sync.Pool{
		New: func() any {
			s := make([]float32, 0, maxSearchN)
			return &s
		},
	}
	intPool = sync.Pool{
		New: func() any {
			s := make([]int, 0, maxSearchN)
			return &s
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
	p := dotPool.Get().(*[]float32) //nolint:errcheck // sync.Pool invariant: New() always returns *[]float32
	if cap(*p) < n {
		return make([]float32, n)
	}
	return (*p)[:n]
}

func putDots(d []float32) {
	if cap(d) != maxSearchN {
		return
	}
	d = d[:0]
	dotPool.Put(&d)
}

func getInts(n int) []int {
	if n > maxSearchN {
		return make([]int, n)
	}
	p := intPool.Get().(*[]int) //nolint:errcheck // sync.Pool invariant: New() always returns *[]int
	if cap(*p) < n {
		return make([]int, n)
	}
	return (*p)[:n]
}

func putInts(d []int) {
	if cap(d) != maxSearchN {
		return
	}
	d = d[:0]
	intPool.Put(&d)
}

type vectorEntry struct {
	id   string
	vec  []float32
	norm float32
}

// InMemoryVectorIndex holds all vectors in memory with a row-major flatMatrix for batch cosine.
type InMemoryVectorIndex struct {
	db         *sql.DB
	mu         sync.RWMutex
	entries    []vectorEntry
	flatMatrix []float32
	cols       int
	byID       map[string]int
}

func NewInMemoryVectorIndex(db *sql.DB) *InMemoryVectorIndex {
	idx := &InMemoryVectorIndex{db: db, byID: make(map[string]int)}
	idx.load()
	return idx
}

// load pulls every non-archived entity from the DB on startup.
// Vectors are normalized on load so Search can skip per-row norm division.
func (idx *InMemoryVectorIndex) load() {
	rows, err := idx.db.Query(`SELECT e.id, e.embedding FROM entities e WHERE e.archived = 0`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			continue
		}
		if emb := store.BytesToEmbedding(blob); emb != nil {
			NormalizeVector(emb)
			if idx.cols == 0 {
				idx.cols = len(emb)
			}
			idx.byID[id] = len(idx.entries)
			idx.entries = append(idx.entries, vectorEntry{id: id, vec: emb, norm: 1})
			idx.flatMatrix = append(idx.flatMatrix, emb...)
		}
	}
}

func (idx *InMemoryVectorIndex) Search(_ context.Context, queryEmbedding []float32, limit int) ([]string, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
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
	out := make([]string, limit)
	for i := 0; i < limit; i++ {
		out[i] = idx.entries[idxs[i]].id
	}
	return out, nil
}

func (idx *InMemoryVectorIndex) SearchBatch(_ context.Context, queries [][]float32, limit int) ([][]string, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
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
			ids[i] = idx.entries[idxs[i]].id
		}
		out[qi] = ids
	}
	return out, nil
}

// Store updates or appends the entry; vec is assumed normalized by caller (dim is sticky from load).
func (idx *InMemoryVectorIndex) Store(_ context.Context, id string, vec []float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	stored := make([]float32, len(vec))
	copy(stored, vec)
	entry := vectorEntry{id: id, vec: stored, norm: 1}
	if i, ok := idx.byID[id]; ok {
		idx.entries[i] = entry
		if idx.cols > 0 {
			copy(idx.flatMatrix[i*idx.cols:(i+1)*idx.cols], stored)
		}
	} else {
		if idx.cols == 0 {
			idx.cols = len(stored)
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
	for _, id := range ids {
		if i, ok := idx.byID[id]; ok {
			delete(idx.byID, id)
			idx.entries[i].vec = nil
		}
	}
	return nil
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
