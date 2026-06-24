// Package vector provides vector operations: cosine similarity, quantization, search, and the in-memory index.
package vector

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sync"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// NewIndex creates a VectorIndex for the given backend.
func NewIndex(backend string, db *sql.DB, dim int) core.VectorIndex {
	if backend == "in-memory" || backend == "" {
		return NewInMemoryVectorIndex(db)
	}
	slog.Warn("vector backend not found, falling back to in-memory", "requested", backend)
	return NewInMemoryVectorIndex(db)
}

// SearchByVector finds the topK entities most similar to queryEmbedding.
func SearchByVector(db *sql.DB, vi core.VectorIndex, queryEmbedding []float32, topK int) ([]core.SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}
	if topK > 500 {
		topK = 500
	}
	ids, err := vi.Search(context.Background(), queryEmbedding, topK)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	phs, args := store.InClauseArgs(ids)
	rows, err := db.Query(fmt.Sprintf(`SELECT id, category, content, embedding, updated_at, last_accessed_at FROM entities WHERE id IN (%s) AND archived = 0`, phs), args...)
	if err != nil {
		return nil, fmt.Errorf("fetch entities: %w", err)
	}
	defer rows.Close()
	var results []core.SearchResult
	for rows.Next() {
		var e core.Entity
		var embBytes []byte
		var lastAcc sql.NullTime
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &embBytes, &e.UpdatedAt, &lastAcc); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		if lastAcc.Valid {
			e.LastAccessedAt = &lastAcc.Time
		}
		sim := float32(0)
		if len(embBytes) > 0 {
			if emb, err := store.DecodeVector(embBytes, len(queryEmbedding)); err == nil {
				sim = CosineSimilarity(queryEmbedding, emb)
			}
		}
		results = append(results, core.SearchResult{Entity: e, Similarity: sim})
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// AddEdgeWithAutoCreate creates an edge, auto-creating missing entities.
func AddEdgeWithAutoCreate(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, src, dst, rel string) error {
	for _, id := range []string{src, dst} {
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM entities WHERE id = ?)", id).Scan(&exists)
		if !exists {
			embedding, err := embedder.Embed(ctx, id)
			if err != nil {
				return fmt.Errorf("embed placeholder %q: %w", id, err)
			}
			if err := store.StoreEntityWithEmbedding(db, vi, core.DefaultSchemaConfig(false), core.Entity{
				ID: id, Category: "world", Content: id, Embedding: embedding,
			}); err != nil {
				return fmt.Errorf("store placeholder %q: %w", id, err)
			}
		}
	}
	return store.AddEdge(db, src, dst, rel, 1.0)
}

// AutoLinkEdges links a new entity to the 3 closest existing entities.
func AutoLinkEdges(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, newID string, newEmbedding []float32) error {
	if len(newEmbedding) == 0 {
		return fmt.Errorf("empty embedding for %s", newID)
	}
	results, err := SearchByVector(db, vi, newEmbedding, 3)
	if err != nil {
		return fmt.Errorf("auto-link search: %w", err)
	}
	inserted := 0
	for _, r := range results {
		if inserted >= 3 {
			break
		}
		if r.Entity.ID == newID {
			continue
		}
		if r.Similarity <= 0.85 {
			continue
		}
		db.ExecContext(ctx, `INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, 'related_to', 1.0)`, newID, r.Entity.ID)
		inserted++
	}
	return nil
}

// --- Cosine similarity functions ---

func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(normA)*float64(normB)))
}

func CosineSimilarityWithNorm(a, b []float32, normB float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(normA))*float64(normB))
}

func VectorNorm(v []float32) float32 {
	var sum float32
	for _, x := range v {
		sum += x * x
	}
	return float32(math.Sqrt(float64(sum)))
}

func NormalizeVector(v []float32) {
	n := VectorNorm(v)
	if n == 0 {
		return
	}
	for i := range v {
		v[i] /= n
	}
}

func BatchDotProducts(query []float32, matrix []float32, rows, cols int, dot []float32) {
	for r := 0; r < rows; r++ {
		var d float32
		for c := 0; c < cols; c++ {
			d += query[c] * matrix[r*cols+c]
		}
		dot[r] = d
	}
}

// --- Quantization ---

type QuantizedVector struct {
	Min   float32 `json:"min"`
	Max   float32 `json:"max"`
	Codes []int8  `json:"codes"`
}

func QuantizeVector(v []float32) QuantizedVector {
	if len(v) == 0 {
		return QuantizedVector{}
	}
	min, max := v[0], v[0]
	for _, x := range v {
		if x < min {
			min = x
		}
		if x > max {
			max = x
		}
	}
	scale := float32(127) / (max - min)
	if max == min {
		scale = 1
	}
	codes := make([]int8, len(v))
	for i, x := range v {
		codes[i] = int8((x - min) * scale)
	}
	return QuantizedVector{Min: min, Max: max, Codes: codes}
}

func DequantizeVector(qv QuantizedVector) []float32 {
	v := make([]float32, len(qv.Codes))
	scale := (qv.Max - qv.Min) / 127
	if qv.Max == qv.Min {
		scale = 1
	}
	for i, c := range qv.Codes {
		v[i] = qv.Min + float32(c)*scale
	}
	return v
}

func QuantizedToBytes(qv QuantizedVector) []byte {
	buf := make([]byte, 8+len(qv.Codes))
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(qv.Min))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(qv.Max))
	for i, c := range qv.Codes {
		buf[8+i] = byte(c)
	}
	return buf
}

func BytesToQuantized(data []byte) (QuantizedVector, error) {
	if len(data) < 8 {
		return QuantizedVector{}, fmt.Errorf("too short")
	}
	min := math.Float32frombits(binary.LittleEndian.Uint32(data[0:4]))
	max := math.Float32frombits(binary.LittleEndian.Uint32(data[4:8]))
	codes := make([]int8, len(data)-8)
	for i := range codes {
		codes[i] = int8(data[8+i])
	}
	return QuantizedVector{Min: min, Max: max, Codes: codes}, nil
}

// --- InMemoryVectorIndex ---

type vectorEntry struct {
	id   string
	vec  []float32
	norm float32
}

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
	sortByScore(idxs, dots)
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
		sortByScore(idxs, dots)
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

func (idx *InMemoryVectorIndex) Store(_ context.Context, id string, vec []float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	entry := vectorEntry{id: id, vec: vec, norm: 1}
	if i, ok := idx.byID[id]; ok {
		idx.entries[i] = entry
		copy(idx.flatMatrix[i*idx.cols:(i+1)*idx.cols], vec)
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
			copy(idx.flatMatrix[i*idx.cols:(i+1)*idx.cols], p.Vec)
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

func sortByScore(idxs []int, scores []float32) {
	for i := 0; i < len(idxs); i++ {
		for j := i + 1; j < len(idxs); j++ {
			if scores[idxs[j]] > scores[idxs[i]] {
				idxs[i], idxs[j] = idxs[j], idxs[i]
			}
		}
	}
}
