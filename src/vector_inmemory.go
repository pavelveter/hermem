package main

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
)

type vectorEntry struct {
	id   string
	vec  []float32
	norm float32
}

type InMemoryVectorIndex struct {
	db      *sql.DB
	mu      sync.RWMutex
	entries []vectorEntry
	byID    map[string]int
}

func NewInMemoryVectorIndex(db *sql.DB) *InMemoryVectorIndex {
	idx := &InMemoryVectorIndex{
		db:   db,
		byID: make(map[string]int),
	}
	idx.load()
	return idx
}

func (idx *InMemoryVectorIndex) load() {
	rows, err := idx.db.Query(`SELECT id, embedding FROM entities WHERE embedding IS NOT NULL`)
	if err != nil {
		return
	}
	defer rows.Close()

	idx.mu.Lock()
	defer idx.mu.Unlock()

	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			continue
		}
		if len(blob) == 0 {
			continue
		}
		if emb := BytesToEmbedding(blob); emb != nil {
			idx.byID[id] = len(idx.entries)
			idx.entries = append(idx.entries, vectorEntry{
				id:   id,
				vec:  emb,
				norm: vectorNorm(emb),
			})
		}
	}
}

// vectorNorm is a small inline wrapper that keeps the import clean.
func vectorNorm(v []float32) float32 {
	if len(v) == 0 {
		return 0
	}
	return VectorNorm(v)
}

func (idx *InMemoryVectorIndex) Search(_ context.Context, queryEmbedding []float32, topK int) ([]string, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	n := len(idx.entries)
	if n == 0 {
		return nil, nil
	}

	cols := len(queryEmbedding)
	queryNorm := vectorNorm(queryEmbedding)
	if queryNorm == 0 {
		return nil, fmt.Errorf("zero query embedding")
	}

	// Build flat matrix for batch dot products.
	matrix := make([]float32, n*cols)
	for i, e := range idx.entries {
		copy(matrix[i*cols:(i+1)*cols], e.vec)
	}

	dots := make([]float32, n)
	BatchDotProducts(queryEmbedding, matrix, n, cols, dots)

	type candidate struct {
		id         string
		similarity float32
	}
	candidates := make([]candidate, n)
	for i, e := range idx.entries {
		sim := dots[i] / (queryNorm * e.norm)
		candidates[i] = candidate{id: e.id, similarity: sim}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].similarity > candidates[j].similarity
	})
	if topK > 0 && len(candidates) > topK {
		candidates = candidates[:topK]
	}

	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.id
	}
	return ids, nil
}

func (idx *InMemoryVectorIndex) SearchBatch(_ context.Context, queries [][]float32, limit int) ([][]string, error) {
	if len(queries) == 0 {
		return nil, nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	n := len(idx.entries)
	if n == 0 {
		results := make([][]string, len(queries))
		return results, nil
	}

	cols := len(queries[0])

	// Build flat matrix once.
	matrix := make([]float32, n*cols)
	for i, e := range idx.entries {
		copy(matrix[i*cols:(i+1)*cols], e.vec)
	}

	type score struct {
		id  string
		sim float32
	}

	// Process each query with one batch call.
	results := make([][]string, len(queries))
	dots := make([]float32, n)

	for qi, q := range queries {
		if len(q) == 0 {
			return nil, fmt.Errorf("empty query embedding in batch")
		}
		queryNorm := vectorNorm(q)
		if queryNorm == 0 {
			return nil, fmt.Errorf("zero query embedding")
		}

		BatchDotProducts(q, matrix, n, cols, dots)

		top := make([]score, 0, limit)
		for i, e := range idx.entries {
			sim := dots[i] / (queryNorm * e.norm)
			t := top
			if len(t) < limit {
				top = append(t, score{id: e.id, sim: sim})
			} else if sim > t[len(t)-1].sim {
				t[len(t)-1] = score{id: e.id, sim: sim}
			} else {
				continue
			}
			t = top
			for j := len(t) - 1; j > 0 && t[j].sim > t[j-1].sim; j-- {
				t[j], t[j-1] = t[j-1], t[j]
			}
		}

		ids := make([]string, len(top))
		for i, s := range top {
			ids[i] = s.id
		}
		results[qi] = ids
	}
	return results, nil
}

func (idx *InMemoryVectorIndex) Store(_ context.Context, id string, vec []float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entry := vectorEntry{id: id, vec: vec, norm: vectorNorm(vec)}
	if i, ok := idx.byID[id]; ok {
		idx.entries[i] = entry
	} else {
		idx.byID[id] = len(idx.entries)
		idx.entries = append(idx.entries, entry)
	}
	return nil
}

func (idx *InMemoryVectorIndex) Remove(_ context.Context, ids []string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	for _, id := range ids {
		pos, ok := idx.byID[id]
		if !ok {
			continue
		}
		lastIdx := len(idx.entries) - 1
		lastEntry := idx.entries[lastIdx]

		idx.entries[pos] = lastEntry
		idx.byID[lastEntry.id] = pos

		idx.entries = idx.entries[:lastIdx]
		delete(idx.byID, id)
	}
	return nil
}
