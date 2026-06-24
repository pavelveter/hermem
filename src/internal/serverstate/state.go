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
type State struct {
	Schema             core.SchemaConfig
	ValidCategories    map[string]bool
	ValidRelationTypes map[string]bool
	DepthCeiling       int
	MaxRetrievedNodes  int
	RankingWeight      core.RankingWeight
	Reranker           core.Reranker
}

// New folds the live-config tuple into a self-consistent snapshot. Empty
// nil maps are replaced with empty maps so handlers can index ValidCategories
// and ValidRelationTypes without nil-checks.
func New(schema core.SchemaConfig, depthCeiling, maxRetrieved int, ranking core.RankingWeight, reranker core.Reranker) *State {
	cats := schema.AllowedCategories
	if cats == nil {
		cats = map[string]bool{}
	}
	rels := schema.AllowedRelations
	if rels == nil {
		rels = map[string]bool{}
	}
	return &State{
		Schema:             schema,
		ValidCategories:    cats,
		ValidRelationTypes: rels,
		DepthCeiling:       depthCeiling,
		MaxRetrievedNodes:  maxRetrieved,
		RankingWeight:      ranking,
		Reranker:           reranker,
	}
}
