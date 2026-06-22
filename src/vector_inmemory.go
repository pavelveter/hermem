package main

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
)

type vectorEntry struct {
	id  string
	vec []float32
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
			idx.entries = append(idx.entries, vectorEntry{id: id, vec: emb})
		}
	}
}

func (idx *InMemoryVectorIndex) Search(_ context.Context, queryEmbedding []float32, topK int) ([]string, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	type candidate struct {
		id         string
		similarity float32
	}
	candidates := make([]candidate, 0, len(idx.entries))

	for _, e := range idx.entries {
		sim := CosineSimilarity(queryEmbedding, e.vec)
		candidates = append(candidates, candidate{id: e.id, similarity: sim})
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
	for _, q := range queries {
		if len(q) == 0 {
			return nil, fmt.Errorf("empty query embedding in batch")
		}
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	type score struct {
		id  string
		sim float32
	}
	top := make([][]score, len(queries))
	for i := range top {
		top[i] = make([]score, 0, limit)
	}

	for _, e := range idx.entries {
		for qi, q := range queries {
			sim := CosineSimilarity(q, e.vec)
			t := top[qi]
			if len(t) < limit {
				top[qi] = append(t, score{id: e.id, sim: sim})
			} else if sim > t[len(t)-1].sim {
				t[len(t)-1] = score{id: e.id, sim: sim}
			} else {
				continue
			}
			t = top[qi]
			for j := len(t) - 1; j > 0 && t[j].sim > t[j-1].sim; j-- {
				t[j], t[j-1] = t[j-1], t[j]
			}
		}
	}

	results := make([][]string, len(queries))
	for qi, t := range top {
		ids := make([]string, len(t))
		for i, s := range t {
			ids[i] = s.id
		}
		results[qi] = ids
	}
	return results, nil
}

func (idx *InMemoryVectorIndex) Store(_ context.Context, id string, vec []float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if i, ok := idx.byID[id]; ok {
		idx.entries[i].vec = vec
	} else {
		idx.byID[id] = len(idx.entries)
		idx.entries = append(idx.entries, vectorEntry{id: id, vec: vec})
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


