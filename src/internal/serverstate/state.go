// Package serverstate holds the runtime config snapshot that Server
// atomically swaps on SIGHUP. It lives as a leaf package (imports nothing
// from internal/) so that:
//
//   - internal/server can import it,
//   - internal/server/retrieval, /task, /memory, /admin can import it,
//
// without any of those packages needing to import each other or back into
// internal/server (which would be a cycle, since internal/server's shell
// mounts those sub-package services).
package serverstate

import "github.com/pavelveter/hermem/src/internal/core"

// State bundles every piece of runtime config that handlers read while
// SIGHUP may concurrently swap it. State.Store() / State.Load() at the
// atomic.Pointer level are what make that swap race-free — handlers always
// observe a self-consistent snapshot.
//
// Why ranking and reranker live HERE (not on RetrievalContextOptions at the
// per-call level): pre-refactor handlers read them from s.RetrievalOpts while
// SIGHUP wrote directly to that struct — a textbook data race. Routing both
// reads and writes through State.Load() / State.Store() eliminates it.
//
// Per-call overrides (e.g. MaxDepth on a Retrieve request) STILL stay on
// RetrieveContextOptions; only the SIGHUP-swapped defaults live here.
//
// Generation is the concurrency-stamp for cross-state transactions. State
// values produced by serverstate.New() have Generation=0; Ref stamps them
// with strictly-positive monotonic integers at NewRef (1) and Store (2,
// 3, …). A handler that captures state.Generation at request start
// re-checks s.Refs.Load().Generation immediately before committing a
// write derived from that snapshot; a mismatch means a SIGHUP swapped
// the schema out from under the handler, which must return 409 Conflict.
// Generation=0 is reserved for "produced by New(), not yet stamped by
// Ref" — handlers must not rely on it as a sentinel; the first handler
// read happens AFTER Ref wires the State into the lifecycle.
type State struct {
	Schema             core.SchemaConfig
	ValidCategories    map[string]bool
	ValidRelationTypes map[string]bool
	DepthCeiling       int
	MaxRetrievedNodes  int
	TokenBudget        int
	RankingWeight      core.RankingWeight
	Reranker           core.Reranker
	Generation         uint64
}

// New folds the live-config tuple into a self-consistent snapshot. Maps inside
// Schema AND the ValidCategories / ValidRelationTypes cache are deep-cloned
// so that concurrent SIGHUP-driven mutations on the source maps cannot alias
// into a State that's already been handed to in-flight handlers — handlers
// iterate over State without contending with a SIGHUP writer. Nil maps in
// the source copy to non-nil empty maps so handlers index freely without
// nil-checks.
func New(schema core.SchemaConfig, depthCeiling, maxRetrieved int, ranking core.RankingWeight, reranker core.Reranker) *State {
	return &State{
		Schema:             cloneSchema(schema),
		ValidCategories:    cloneBoolMap(schema.AllowedCategories),
		ValidRelationTypes: cloneBoolMap(schema.AllowedRelations),
		DepthCeiling:       depthCeiling,
		MaxRetrievedNodes:  maxRetrieved,
		RankingWeight:      ranking,
		Reranker:           reranker,
	}
}

// cloneBoolMap returns a copy of src. nil input produces an empty (but non-nil)
// map so callers can iteratively use the result without nil-checks. Mutating
// the returned map never affects the source.
func cloneBoolMap(src map[string]bool) map[string]bool {
	if src == nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// cloneSchema returns a SchemaConfig whose map fields reference independent
// heap maps, not the source's maps. Slices and scalar fields are copied by
// value (the SchemaConfig slice types are immutable-value slices like
// ValidStateOrder; if a future field adds a mutable slice, extend this
// helper). The two Allowed* maps are cloned in addition to Stateful…
// and Valid… because handlers in retrieval/ hold schema maps directly
// (not just the State.Valid* cache) and a partial clone would re-introduce
// the same aliasing bug class for those callers.
func cloneSchema(s core.SchemaConfig) core.SchemaConfig {
	out := s // shallow copy of scalars; maps/slices below are replaced.
	out.AllowedCategories = cloneBoolMap(s.AllowedCategories)
	out.AllowedRelations = cloneBoolMap(s.AllowedRelations)
	out.StatefulCategories = cloneBoolMap(s.StatefulCategories)
	out.ValidStates = cloneBoolMap(s.ValidStates)
	// ValidStateOrder is read-only-by-convention; copy-by-reference keeps
	// the immutability invariant (callers rebuild the slice on reshuffle).
	return out
}
