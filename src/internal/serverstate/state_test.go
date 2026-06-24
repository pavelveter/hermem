package serverstate

import (
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
