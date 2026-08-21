package vector

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
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

// memoryRecord is one stored vector with its filterable metadata.
type memoryRecord struct {
	vec  []float32 // normalized at upsert time
	meta map[string]string
}

// inMemoryVectorStore is the canonical public-contract implementation of
// spi.VectorStore: namespace-scoped brute-force cosine search with
// metadata filters, sticky per-namespace dimensions, explicit capacity
// reporting (no silent eviction), and partial-batch failure semantics.
type inMemoryVectorStore struct {
	mu   sync.RWMutex
	max  int
	dims map[string]int
	recs map[string]map[string]memoryRecord
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
		recs: make(map[string]map[string]memoryRecord),
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

	query := make([]float32, len(req.Vector))
	copy(query, req.Vector)
	NormalizeVector(query)
	queryNorm := VectorNorm(query)

	hits := make([]spi.Hit, 0, len(s.recs[req.Namespace]))
	for id, rec := range s.recs[req.Namespace] {
		if !matchesFilter(rec.meta, req.Filter) {
			continue
		}
		score := float32(0)
		if queryNorm > 0 {
			dot := make([]float32, 1)
			BatchDotProducts(query, rec.vec, 1, len(rec.vec), dot)
			score = dot[0]
		}
		hits = append(hits, spi.Hit{ID: id, Score: score})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if req.Limit < len(hits) {
		hits = hits[:req.Limit]
	}
	return hits, nil
}

func (s *inMemoryVectorStore) Upsert(_ context.Context, records []spi.VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		failed    []string
		firstErr  error
		applied   int
		pendingNs = map[string]bool{}
	)
	for _, record := range records {
		if err := s.upsertOne(record); err != nil {
			failed = append(failed, record.ID)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		pendingNs[record.Namespace] = true
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

	ns, ok := s.recs[record.Namespace]
	if !ok {
		ns = make(map[string]memoryRecord)
		s.recs[record.Namespace] = ns
	}
	if _, exists := ns[record.ID]; !exists && s.max > 0 && s.totalLocked() >= s.max {
		return &spi.CapacityError{
			Namespace: record.Namespace,
			Reason:    fmt.Sprintf("in-memory store at capacity (%d records)", s.max),
		}
	}

	vec := make([]float32, len(record.Vector))
	copy(vec, record.Vector)
	NormalizeVector(vec)
	meta := make(map[string]string, len(record.Metadata))
	for k, v := range record.Metadata {
		meta[k] = v
	}
	ns[record.ID] = memoryRecord{vec: vec, meta: meta}
	s.dims[record.Namespace] = dim
	return nil
}

func (s *inMemoryVectorStore) Delete(_ context.Context, req spi.DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns, ok := s.recs[req.Namespace]
	if !ok {
		return nil
	}
	for _, id := range req.IDs {
		delete(ns, id)
	}
	return nil
}

func (s *inMemoryVectorStore) Stats(_ context.Context, namespace string) (spi.VectorStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return spi.VectorStats{
		Namespace:  namespace,
		Count:      len(s.recs[namespace]),
		Dimensions: s.dims[namespace],
	}, nil
}

func (s *inMemoryVectorStore) totalLocked() int {
	total := 0
	for _, ns := range s.recs {
		total += len(ns)
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
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil {
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
