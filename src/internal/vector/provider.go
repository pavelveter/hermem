package vector

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"

	"github.com/pavelveter/hermem/pkg/spi"
)

// InMemoryVectorStoreProvider is the registry name of the built-in
// in-memory VectorStore provider. It matches the config VectorBackend
// value that selects this backend.
const InMemoryVectorStoreProvider = "in-memory"

// InMemoryVectorStoreFactory returns the explicit built-in VectorStore factory
// used by application-owned provider composition. The database handle is part
// of the factory signature for parity with persistence-backed backends; the
// in-memory store itself keeps all records in process memory and ignores it.
func InMemoryVectorStoreFactory(db *sql.DB, maxVectors int) spi.VectorStoreFactory {
	return func(context.Context, spi.ProviderConfig) (spi.VectorStore, error) {
		return NewInMemoryVectorStore(db, maxVectors), nil
	}
}

var _ spi.VectorStoreFactory = InMemoryVectorStoreFactory(nil, 0)

// memoryNamespace is the storage for one namespace: a contiguous row-major
// matrix plus parallel ID and metadata slices. Keeping vectors contiguous lets
// Search hand the whole namespace to BatchDotProducts without copying. The
// previous map-of-slices layout copied every vector into a fresh matrix on each
// query (31 MB allocated per search at 10k×768).
type memoryNamespace struct {
	dim  int
	ids  []string
	vecs []float32 // len(ids)*dim, row-major, unit-normalized at upsert
	meta []map[string]string
	pos  map[string]int // entity ID -> row
}

func newMemoryNamespace(dim int) *memoryNamespace {
	return &memoryNamespace{dim: dim, pos: make(map[string]int)}
}

func (n *memoryNamespace) len() int { return len(n.ids) }

// put inserts or overwrites id. vec must already be normalized and have n.dim
// elements; meta ownership transfers to the namespace.
func (n *memoryNamespace) put(id string, vec []float32, meta map[string]string) {
	if row, ok := n.pos[id]; ok {
		copy(n.vecs[row*n.dim:], vec)
		n.meta[row] = meta
		return
	}
	n.pos[id] = len(n.ids)
	n.ids = append(n.ids, id)
	n.vecs = append(n.vecs, vec...)
	n.meta = append(n.meta, meta)
}

// remove drops id with a swap-remove so the matrix stays dense. Deleting a
// missing ID is a no-op, matching the previous map semantics.
func (n *memoryNamespace) remove(id string) {
	row, ok := n.pos[id]
	if !ok {
		return
	}
	last := len(n.ids) - 1
	if row != last {
		n.ids[row] = n.ids[last]
		copy(n.vecs[row*n.dim:], n.vecs[last*n.dim:])
		n.meta[row] = n.meta[last]
		n.pos[n.ids[row]] = row
	}
	n.ids[last] = ""
	n.meta[last] = nil
	n.ids = n.ids[:last]
	n.meta = n.meta[:last]
	n.vecs = n.vecs[:last*n.dim]
	delete(n.pos, id)
}

// filteredScores scores only the rows matching filter. This is the one path
// that copies vectors: a filter breaks the contiguity the unfiltered path
// relies on.
func (n *memoryNamespace) filteredScores(query []float32, queryNorm float32, filter spi.Filter) ([]string, []float32) {
	ids := make([]string, 0, len(n.ids))
	matrix := make([]float32, 0, len(n.ids)*n.dim)
	for row := range n.ids {
		if !matchesFilter(n.meta[row], filter) {
			continue
		}
		ids = append(ids, n.ids[row])
		matrix = append(matrix, n.vecs[row*n.dim:(row+1)*n.dim]...)
	}
	scores := make([]float32, len(ids))
	if queryNorm > 0 && len(matrix) > 0 {
		BatchDotProducts(query, matrix, len(ids), n.dim, scores)
	}
	return ids, scores
}

// inMemoryVectorStore is the canonical public-contract implementation of
// spi.VectorStore: namespace-scoped brute-force cosine search with
// metadata filters, sticky per-namespace dimensions, explicit capacity
// reporting (no silent eviction), and partial-batch failure semantics.
type inMemoryVectorStore struct {
	mu   sync.RWMutex
	max  int
	dims map[string]int
	ns   map[string]*memoryNamespace
}

var _ spi.VectorStore = (*inMemoryVectorStore)(nil)

// NewInMemoryVectorStore builds an empty public VectorStore. maxVectors
// caps the total number of stored records across namespaces; values <= 0
// mean unlimited. The db handle is accepted for factory-signature parity
// and is not used.
func NewInMemoryVectorStore(_ *sql.DB, maxVectors int) spi.VectorStore {
	return &inMemoryVectorStore{
		max:  maxVectors,
		dims: make(map[string]int),
		ns:   make(map[string]*memoryNamespace),
	}
}

func (s *inMemoryVectorStore) Search(_ context.Context, req spi.SearchRequest) ([]spi.Hit, error) {
	if req.Limit < 0 {
		return nil, fmt.Errorf("%w: limit %d must be >= 0", ErrLimitOutOfRange, req.Limit)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	dim := s.dims[req.Namespace]
	if dim != 0 && len(req.Vector) != dim {
		return nil, &spi.DimensionError{Namespace: req.Namespace, Want: dim, Got: len(req.Vector)}
	}
	ns := s.ns[req.Namespace]
	if ns == nil || ns.len() == 0 {
		return []spi.Hit{}, nil
	}

	query := make([]float32, len(req.Vector))
	copy(query, req.Vector)
	NormalizeVector(query)
	queryNorm := VectorNorm(query)

	ids := ns.ids
	var scores []float32
	if len(req.Filter.Equals) == 0 && len(req.Filter.Ranges) == 0 {
		scores = make([]float32, len(ids))
		if queryNorm > 0 && dim > 0 {
			BatchDotProducts(query, ns.vecs, len(ids), dim, scores)
		}
	} else {
		ids, scores = ns.filteredScores(query, queryNorm, req.Filter)
	}

	hits := topHits(ids, scores, req.Limit)
	return hits, nil
}

// topHits returns the k highest scores with their IDs, ordered by score
// descending. Bounded insertion beats sorting the whole namespace when k is
// small (the normal case) and avoids materializing a hit for every row.
func topHits(ids []string, scores []float32, k int) []spi.Hit {
	if k <= 0 {
		return []spi.Hit{}
	}
	if k >= len(ids) {
		hits := make([]spi.Hit, len(ids))
		for i, id := range ids {
			hits[i] = spi.Hit{ID: id, Score: scores[i]}
		}
		sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
		return hits
	}
	best := make([]spi.Hit, 0, k+1)
	for i, id := range ids {
		score := scores[i]
		if len(best) == k && score <= best[k-1].Score {
			continue
		}
		at := sort.Search(len(best), func(j int) bool { return best[j].Score <= score })
		if at >= k {
			continue
		}
		best = append(best, spi.Hit{})
		copy(best[at+1:], best[at:])
		best[at] = spi.Hit{ID: id, Score: score}
		if len(best) > k {
			best = best[:k]
		}
	}
	return best
}

func (s *inMemoryVectorStore) Upsert(_ context.Context, records []spi.VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		failed   []string
		firstErr error
		applied  int
	)
	for _, record := range records {
		if err := s.upsertOne(record); err != nil {
			failed = append(failed, record.ID)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		applied++
	}
	if len(failed) > 0 {
		return &spi.PartialBatchError{FailedIDs: failed, Cause: firstErr}
	}
	return nil
}

func (s *inMemoryVectorStore) upsertOne(record spi.VectorRecord) error {
	dim := s.dims[record.Namespace]
	if dim == 0 {
		dim = len(record.Vector)
	}
	if len(record.Vector) != dim {
		return &spi.DimensionError{Namespace: record.Namespace, Want: dim, Got: len(record.Vector)}
	}

	ns, ok := s.ns[record.Namespace]
	if !ok {
		ns = newMemoryNamespace(dim)
		s.ns[record.Namespace] = ns
	}
	if ns.dim != dim {
		return &spi.DimensionError{Namespace: record.Namespace, Want: ns.dim, Got: dim}
	}
	if _, exists := ns.pos[record.ID]; !exists && s.max > 0 && s.totalLocked() >= s.max {
		return &spi.CapacityError{
			Namespace: record.Namespace,
			Reason:    fmt.Sprintf("in-memory store at capacity (%d records)", s.max),
		}
	}

	vec := make([]float32, dim)
	copy(vec, record.Vector)
	NormalizeVector(vec)
	meta := make(map[string]string, len(record.Metadata))
	for k, v := range record.Metadata {
		meta[k] = v
	}
	ns.put(record.ID, vec, meta)
	s.dims[record.Namespace] = dim
	return nil
}

func (s *inMemoryVectorStore) Delete(_ context.Context, req spi.DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns, ok := s.ns[req.Namespace]
	if !ok {
		return nil
	}
	for _, id := range req.IDs {
		ns.remove(id)
	}
	return nil
}

func (s *inMemoryVectorStore) Stats(_ context.Context, namespace string) (spi.VectorStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	if ns, ok := s.ns[namespace]; ok {
		count = ns.len()
	}
	return spi.VectorStats{
		Namespace:  namespace,
		Count:      count,
		Dimensions: s.dims[namespace],
	}, nil
}

func (s *inMemoryVectorStore) totalLocked() int {
	total := 0
	for _, ns := range s.ns {
		total += ns.len()
	}
	return total
}

// matchesFilter reports whether metadata satisfies the backend-neutral
// filter: every Equals pair must match exactly and every Range bound
// must contain the parsed numeric value. Records missing a filtered key
// are excluded. Unparseable numeric metadata is treated as a miss.
func matchesFilter(meta map[string]string, filter spi.Filter) bool {
	for key, want := range filter.Equals {
		if meta[key] != want {
			return false
		}
	}
	for key, rng := range filter.Ranges {
		value, err := parseFloat(meta[key])
		if err != nil {
			return false
		}
		if rng.Min != nil && value < *rng.Min {
			return false
		}
		if rng.Max != nil && value > *rng.Max {
			return false
		}
	}
	return true
}

func parseFloat(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty numeric metadata")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("non-finite numeric metadata")
	}
	return f, nil
}

// sqliteVecVectorStore adapts the sqlite-vec backend to the canonical
// public contract. Filters are unsupported (equality/range metadata is a
// public-contract concept the extension does not evaluate); namespace is
// fixed to spi.DefaultNamespace, matching the single-tenant runtime.
type sqliteVecVectorStore struct {
	idx *SQLiteVecIndex
}

var _ spi.VectorStore = (*sqliteVecVectorStore)(nil)

func (s *sqliteVecVectorStore) Search(ctx context.Context, req spi.SearchRequest) ([]spi.Hit, error) {
	//nolint:staticcheck // SA4023: the sqlite-vec engine is currently a stub that always errors; keep the plumbing for the real implementation
	ids, err := s.idx.Search(ctx, req.Vector, req.Limit)
	//nolint:staticcheck // SA4023: the sqlite-vec engine is currently a stub that always errors; keep the plumbing for the real implementation
	if err != nil {
		return nil, err
	}
	hits := make([]spi.Hit, 0, len(ids))
	for _, id := range ids {
		hits = append(hits, spi.Hit{ID: id})
	}
	return hits, nil
}

func (s *sqliteVecVectorStore) Upsert(ctx context.Context, records []spi.VectorRecord) error {
	for _, record := range records {
		//nolint:staticcheck // SA4023: see Search — stub engine, real implementation keeps this check meaningful
		if err := s.idx.Store(ctx, record.ID, record.Vector); err != nil {
			return err
		}
	}
	return nil
}

func (s *sqliteVecVectorStore) Delete(ctx context.Context, req spi.DeleteRequest) error {
	return s.idx.Remove(ctx, req.IDs)
}

func (s *sqliteVecVectorStore) Stats(_ context.Context, namespace string) (spi.VectorStats, error) {
	return spi.VectorStats{}, spi.ErrUnsupported
}

// NewStore constructs the configured backend's public VectorStore view.
// It is the composition entry point that replaces the former
// spiadapter.VectorStore(vector.NewIndex(...)) wrap.
func NewStore(backend string, db *sql.DB, dim int) (spi.VectorStore, error) {
	switch backend {
	case "sqlite-vec":
		idx, err := NewSQLiteVecIndex(db, dim)
		if err != nil {
			// Preserve the historical degrade: extension unavailable →
			// fall back to the in-memory backend.
			return NewInMemoryVectorStore(db, 0), nil
		}
		return &sqliteVecVectorStore{idx: idx}, nil
	default: // "in-memory" or ""
		return NewInMemoryVectorStore(db, 0), nil
	}
}
