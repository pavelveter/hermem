package server

import "github.com/pavelveter/hermem/src/internal/core"

// ServerState holds every piece of runtime config that handlers read while
// SIGHUP may concurrently swap it. Store()/Load() are atomic, so handlers
// always see a self-consistent snapshot.
//
// Why this lives on the server package (not core): the fields are exactly
// what the server routes read; core.SchemaConfig is the source of truth
// from config; core.RankingWeight/Reranker are the composite from config.
// This struct is the join point for "what config knobs affect request serving".
type ServerState struct {
	Schema             core.SchemaConfig
	ValidCategories    map[string]bool
	ValidRelationTypes map[string]bool
	// DepthCeiling and MaxRetrievedNodes bound Retrieve/Query/Response walks.
	// Per-call MaxDepth from request bodies STILL overrides DepthCeiling for
	// the deepest single request — see retrievalService.ServeRetrieve.
	DepthCeiling      int
	MaxRetrievedNodes int
	// RankingWeight and Reranker are now SIGHUP-swappable to fix the
	// pre-refactor data race where SIGHUP mutated s.RetrievalOpts while
	// handlers read it.
	RankingWeight core.RankingWeight
	Reranker      core.Reranker
}

// newServerState folds the live-config tuple into a self-consistent snapshot.
// All parallel maps are derived from Schema to guarantee the ValidCategory /
// AllowedCategories / SchemaAllowedCategories triplet stays in lock-step.
func newServerState(schema core.SchemaConfig, depthCeiling, maxRetrieved int, ranking core.RankingWeight, reranker core.Reranker) *ServerState {
	cats := schema.AllowedCategories
	if cats == nil {
		cats = map[string]bool{}
	}
	rels := schema.AllowedRelations
	if rels == nil {
		rels = map[string]bool{}
	}
	return &ServerState{
		Schema:             schema,
		ValidCategories:    cats,
		ValidRelationTypes: rels,
		DepthCeiling:       depthCeiling,
		MaxRetrievedNodes:  maxRetrieved,
		RankingWeight:      ranking,
		Reranker:           reranker,
	}
}
