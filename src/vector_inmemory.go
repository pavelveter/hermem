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
	db         *sql.DB
	mu         sync.RWMutex
	entries    []vectorEntry
	byID       map[string]int
	flatMatrix []float32 // row-major: entries[i].vec concatenated
	cols       int       // vector dimension (0 until first entity loaded)
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
			NormalizeVector(emb)
			if idx.cols == 0 {
				idx.cols = len(emb)
			}
			idx.byID[id] = len(idx.entries)
			idx.entries = append(idx.entries, vectorEntry{
				id:   id,
				vec:  emb,
				norm: 1,
			})
			idx.flatMatrix = append(idx.flatMatrix, emb...)
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
	n := len(idx.entries)
	if n == 0 {
		idx.mu.RUnlock()
		return nil, nil
	}
	flatMatrix := idx.flatMatrix
	entries := idx.entries
	cols := idx.cols
	idx.mu.RUnlock()

	queryNorm := vectorNorm(queryEmbedding)
	if queryNorm == 0 {
		return nil, fmt.Errorf("zero query embedding")
	}

	dots := make([]float32, n)
	BatchDotProducts(queryEmbedding, flatMatrix, n, cols, dots)

	for i := range dots {
		dots[i] /= queryNorm
	}

	idxs := make([]int, n)
	for i := range idxs {
		idxs[i] = i
	}
	sort.Slice(idxs, func(i, j int) bool {
		return dots[idxs[i]] > dots[idxs[j]]
	})
	if topK > 0 && topK < n {
		idxs = idxs[:topK]
	}

	ids := make([]string, len(idxs))
	for i, pos := range idxs {
		ids[i] = entries[pos].id
	}
	return ids, nil
}

func (idx *InMemoryVectorIndex) SearchBatch(_ context.Context, queries [][]float32, limit int) ([][]string, error) {
	if len(queries) == 0 {
		return nil, nil
	}

	idx.mu.RLock()
	n := len(idx.entries)
	if n == 0 {
		idx.mu.RUnlock()
		results := make([][]string, len(queries))
		return results, nil
	}
	flatMatrix := idx.flatMatrix
	entries := idx.entries
	cols := idx.cols
	idx.mu.RUnlock()

	dots := make([]float32, n)
	results := make([][]string, len(queries))

	for qi, q := range queries {
		if len(q) == 0 {
			return nil, fmt.Errorf("empty query embedding in batch")
		}
		queryNorm := vectorNorm(q)
		if queryNorm == 0 {
			return nil, fmt.Errorf("zero query embedding")
		}

		BatchDotProducts(q, flatMatrix, n, cols, dots)

		for i := range dots {
			dots[i] /= queryNorm
		}

		idxs := make([]int, n)
		for i := range idxs {
			idxs[i] = i
		}
		sort.Slice(idxs, func(i, j int) bool {
			return dots[idxs[i]] > dots[idxs[j]]
		})
		if limit > 0 && limit < n {
			idxs = idxs[:limit]
		}

		ids := make([]string, len(idxs))
		for i, pos := range idxs {
			ids[i] = entries[pos].id
		}
		results[qi] = ids
	}
	return results, nil
}

func (idx *InMemoryVectorIndex) Store(_ context.Context, id string, vec []float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// vec is already normalized by StoreEntityWithEmbedding caller
	entry := vectorEntry{id: id, vec: vec, norm: 1}
	if i, ok := idx.byID[id]; ok {
		idx.entries[i] = entry
		copy(idx.flatMatrix[i*idx.cols:(i+1)*idx.cols], vec)
	} else {
		idx.byID[id] = len(idx.entries)
		idx.entries = append(idx.entries, entry)
		if idx.cols == 0 {
			idx.cols = len(vec)
		}
		idx.flatMatrix = append(idx.flatMatrix, vec...)
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

		copy(idx.flatMatrix[pos*idx.cols:(pos+1)*idx.cols], idx.flatMatrix[lastIdx*idx.cols:(lastIdx+1)*idx.cols])
		idx.flatMatrix = idx.flatMatrix[:lastIdx*idx.cols]

		delete(idx.byID, id)
	}
	return nil
}
