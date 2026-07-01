package serverstate

import (
	"sync"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// TestNew_NilCategoryMapBecomesEmptyMap — handlers index into
// ValidCategories without nil-checks. A nil map would silently swallow
// the lookup into the void and let invalid categories pass.
func TestNew_NilCategoryMapBecomesEmptyMap(t *testing.T) {
	s := New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil)
	if s.ValidCategories == nil {
		t.Fatal("ValidCategories nil: handlers would panic on map[K]V")
	}
	if len(s.ValidCategories) != 0 {
		t.Fatalf("ValidCategories: want empty map, got %d entries", len(s.ValidCategories))
	}
}

// TestNew_NilRelationMapBecomesEmptyMap — same nil-defense as above.
func TestNew_NilRelationMapBecomesEmptyMap(t *testing.T) {
	s := New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil)
	if s.ValidRelationTypes == nil {
		t.Fatal("ValidRelationTypes nil")
	}
	if len(s.ValidRelationTypes) != 0 {
		t.Fatalf("ValidRelationTypes: want empty map, got %d entries", len(s.ValidRelationTypes))
	}
}

// TestNew_PreservesProvidedMap — if the caller already populated allowed
// categories, New must keep them; defensive conversion is only on nil.
func TestNew_PreservesProvidedMap(t *testing.T) {
	cats := map[string]bool{"world": true, "task": true}
	rels := map[string]bool{"blocked_by": true}
	schema := core.SchemaConfig{AllowedCategories: cats, AllowedRelations: rels}
	s := New(schema, 5, 100, core.RankingWeight{}, nil)
	if !s.ValidCategories["world"] || !s.ValidCategories["task"] {
		t.Fatalf("ValidCategories lost entries: %+v", s.ValidCategories)
	}
	if !s.ValidRelationTypes["blocked_by"] {
		t.Fatalf("ValidRelationTypes lost entries: %+v", s.ValidRelationTypes)
	}
}

// TestNew_RoundTripsDepthBounds — DepthCeiling + MaxRetrievedNodes are
// passed through unchanged. These power the graph walker; losing them
// silently would mean every query walks the full graph.
func TestNew_RoundTripsDepthBounds(t *testing.T) {
	s := New(core.SchemaConfig{}, 7, 250, core.RankingWeight{}, nil)
	if s.DepthCeiling != 7 {
		t.Fatalf("DepthCeiling: want 7, got %d", s.DepthCeiling)
	}
	if s.MaxRetrievedNodes != 250 {
		t.Fatalf("MaxRetrievedNodes: want 250, got %d", s.MaxRetrievedNodes)
	}
}

// TestNew_PreservesRankingAndReranker — ranking weight + reranker must
// come through verbatim. A nil reranker is a valid config (degraded
// ordering) but the field must be the same pointer the caller passed.
func TestNew_PreservesRankingAndReranker(t *testing.T) {
	w := core.RankingWeight{}.WithDefaults()
	s := New(core.SchemaConfig{}, 5, 100, w, nil)
	if s.RankingWeight.VectorWeight != w.VectorWeight {
		t.Fatalf("RankingWeight.VectorWeight: want %v, got %v", w.VectorWeight, s.RankingWeight.VectorWeight)
	}
	if s.Reranker != nil {
		t.Fatal("Reranker: want nil, got non-nil")
	}
}

// TestRef_NewRefStampsInitialStateWithGenerationOne — NewRef must stamp
// the wrapping State with Generation=1 so a hand-constructed State (e.g.
// in a test) still detects any swap against the post-NewRef baseline.
// Skipping 0 preserves "Generation 0 = pre-Ref construction" as a
// diagnostic signal.
func TestRef_NewRefStampsInitialStateWithGenerationOne(t *testing.T) {
	s := New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil)
	if s.Generation != 0 {
		t.Fatalf("pre-NewRef State.Generation: want 0, got %d", s.Generation)
	}
	refs := NewRef(s)
	if s.Generation != 1 {
		t.Fatalf("post-NewRef State.Generation: want 1, got %d", s.Generation)
	}
	if refs.Load().Generation != 1 {
		t.Fatalf("post-NewRef Load().Generation: want 1, got %d", refs.Load().Generation)
	}
}

// TestRef_StoreStampsIncomingStateWithNextGeneration — Ref.Store must
// increment its atomic counter and write the new value onto the incoming
// State BEFORE atomic.Pointer swaps it in. A concurrent reader therefore
// sees (oldState, oldGen) or (newState, newGen) — never an inconsistent
// pair. This is the contract that handlers' IsStale check relies on.
func TestRef_StoreStampsIncomingStateWithNextGeneration(t *testing.T) {
	refs := NewRef(New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil))
	s2 := New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil)
	if s2.Generation != 0 {
		t.Fatalf("s2.Generation pre-Store: want 0, got %d", s2.Generation)
	}
	refs.Store(s2)
	if s2.Generation != 2 {
		t.Fatalf("s2.Generation post-Store: want 2, got %d", s2.Generation)
	}
	if refs.Load().Generation != 2 {
		t.Fatalf("refs.Load().Generation: want 2, got %d", refs.Load().Generation)
	}
}

// TestRef_StoreIsMonotonic — multiple Store calls must produce strictly
// monotonically increasing Generations. If this test ever fails, two
// concurrent Store calls collapsed into one bump and in-flight handlers
// will see stale Generation comparisons.
func TestRef_StoreIsMonotonic(t *testing.T) {
	refs := NewRef(New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil))
	for i := 2; i <= 5; i++ {
		next := New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil)
		refs.Store(next)
		if next.Generation != uint64(i) {
			t.Fatalf("Store #%d: want Generation %d, got %d", i, i, next.Generation)
		}
	}
	if refs.Load().Generation != 5 {
		t.Fatalf("final Load(): want Generation 5, got %d", refs.Load().Generation)
	}
}

// TestRef_IsStaleDetectsSwap — handler contract. A handler that captures
// state.Generation at request start and re-checks before commit must
// observe IsStale=true if a SIGHUP ran in between.
func TestRef_IsStaleDetectsSwap(t *testing.T) {
	refs := NewRef(New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil))
	captured := refs.Load().Generation
	refs.Store(New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil))
	if !refs.IsStale(captured) {
		t.Fatalf("after swap, captured gen %d must report stale", captured)
	}
	current := refs.Load().Generation
	if refs.IsStale(current) {
		t.Fatalf("current gen %d must not report stale", current)
	}
}

// TestNew_SchemaMapsNotAliasedToSource — regression for the partial
// deep-clone bug: pre-fix, State.New cloned ValidCategories and
// ValidRelationTypes but left Schema.Allowed*, Schema.StatefulCategories,
// and Schema.ValidStates pointing at the source maps. A caller mutating
// cfg.AllowedCategories[\"evil\"] = true AFTER New() (and then SIGHUP'ing
// a new State without rebuilding the SchemaConfig from scratch) would
// silently corrupt State.Schema for every State already in the wild.
// This test pins the clone invariant: source mutation AFTER New() must
// NOT leak into State. It also covers AllowedRelations (the relation-side
// analogue: admin adds a new relation_type post-construction) and the
// State-level cache (ValidCategories/ValidRelationTypes) decoupled via
// wave 2's cloneBoolMap.
//
// The drain-not-clone half (asserting source retains its keys after
// cloneSchema returns) is also load-bearing: callers such as the SIGHUP
// path reuse the source SchemaConfig for the next ReloadState; moving
// instead of copying would silently drain the live config mid-reload.
func TestNew_SchemaMapsNotAliasedToSource(t *testing.T) {
	src := core.DefaultSchemaConfig(false)
	s := New(src, 0, 0, core.RankingWeight{}, nil)
	// Mutate SOURCE maps AFTER New() returns. If cloneSchema isolated the
	// clone from the source, the State must be unaffected.
	src.AllowedCategories["evil"] = true
	src.AllowedRelations["evil_rel"] = true
	src.StatefulCategories["evil_stateful"] = true
	src.ValidStates["evil_state"] = true
	if s.Schema.AllowedCategories["evil"] {
		t.Error("State.Schema.AllowedCategories aliases source: mutation leaked through cloneSchema")
	}
	if s.Schema.AllowedRelations["evil_rel"] {
		t.Error("State.Schema.AllowedRelations aliases source")
	}
	if s.Schema.StatefulCategories["evil_stateful"] {
		t.Error("State.Schema.StatefulCategories aliases source")
	}
	if s.Schema.ValidStates["evil_state"] {
		t.Error("State.Schema.ValidStates aliases source")
	}
	// State-level cache committed by 6dd5f2c must also be decoupled.
	if s.ValidCategories["evil"] {
		t.Error("State.ValidCategories aliases source")
	}
	if s.ValidRelationTypes["evil_rel"] {
		t.Error("State.ValidRelationTypes aliases source")
	}
	// Clone must NOT drain the source — it COPYs.
	if !src.AllowedCategories["evil"] || !src.AllowedRelations["evil_rel"] ||
		!src.StatefulCategories["evil_stateful"] || !src.ValidStates["evil_state"] {
		t.Error("cloneSchema drained source: must clone, not move")
	}
}

// TestRef_StoreConcurrentDistinct — load-bearing race-protection test.
// HandlerStore + HandleEdge's IsStale check relies on every concurrent
// Store handing out a distinct Generation from Ref's atomic counter. If
// two Stamps lose a race and both observe the same Generation, two
// handlers in two different snapshots would both pass IsStale, defeating
// the optimistic-concurrency guarantee. Fire N concurrent Store
// goroutines and assert: (a) all N stamped Generation values are distinct,
// (b) Load().Generation equals initial (1) + N (each Store bumped once).
//
// -race-required: this test is intended for `go test -race`. Non-race
// runs lack the timing instrumentation that catches a torn Generation
// read on amd64; the distinctness check still passes because the atomic
// counter serialises, but the redundant point with TestRef_StoreIsMonotonic
// (sequential case) only materialises under -race.
func TestRef_StoreConcurrentDistinct(t *testing.T) {
	refs := NewRef(New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil))
	const N = 100
	states := make([]*State, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			s := New(core.SchemaConfig{}, 0, 0, core.RankingWeight{}, nil)
			refs.Store(s)
			states[i] = s
		}(i)
	}
	wg.Wait()
	seen := make(map[uint64]int, N)
	for i, s := range states {
		if s == nil {
			t.Fatalf("goroutine %d: nil state after Store", i)
		}
		gen := s.Generation
		if prev, dup := seen[gen]; dup {
			t.Errorf("generation %d reused by goroutines %d and %d (atomic counter race)",
				gen, prev, i)
		}
		seen[gen] = i
	}
	gen := refs.Load().Generation
	if gen < 2 || gen > uint64(1+N) {
		t.Fatalf("after %d concurrent Stores: Load().Generation = %d, want [2, %d]",
			N, gen, 1+N)
	}
}
