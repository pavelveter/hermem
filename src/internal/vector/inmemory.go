package vector

import (
	"context"
	"database/sql"
	"sync"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

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
	loaded     bool
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
	NormalizeVector(queryEmbedding)
	queryNorm := VectorNorm(queryEmbedding)
	dots := make([]float32, n)
	BatchDotProducts(queryEmbedding, idx.flatMatrix, n, idx.cols, dots)
	for i := range dots {
		dots[i] /= queryNorm
	}
	idxs := make([]int, n)
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
	out := make([][]string, len(queries))
	for qi, q := range queries {
		NormalizeVector(q)
		qNorm := VectorNorm(q)
		dots := make([]float32, n)
		BatchDotProducts(q, idx.flatMatrix, n, idx.cols, dots)
		for i := range dots {
			dots[i] /= qNorm
		}
		idxs := make([]int, n)
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
	entry := vectorEntry{id: id, vec: vec, norm: 1}
	if i, ok := idx.byID[id]; ok {
		idx.entries[i] = entry
		if idx.cols > 0 {
			copy(idx.flatMatrix[i*idx.cols:(i+1)*idx.cols], vec)
		}
	} else {
		if idx.cols == 0 {
			idx.cols = len(vec)
		}
		idx.byID[id] = len(idx.entries)
		idx.entries = append(idx.entries, entry)
		idx.flatMatrix = append(idx.flatMatrix, vec...)
	}
	return nil
}

func (idx *InMemoryVectorIndex) BulkStore(_ context.Context, pairs []core.BulkPair) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for _, p := range pairs {
		entry := vectorEntry{id: p.ID, vec: p.Vec, norm: 1}
		if i, ok := idx.byID[p.ID]; ok {
			idx.entries[i] = entry
			if idx.cols > 0 {
				copy(idx.flatMatrix[i*idx.cols:(i+1)*idx.cols], p.Vec)
			}
		} else {
			if idx.cols == 0 {
				idx.cols = len(p.Vec)
			}
			idx.byID[p.ID] = len(idx.entries)
			idx.entries = append(idx.entries, entry)
			idx.flatMatrix = append(idx.flatMatrix, p.Vec...)
		}
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
