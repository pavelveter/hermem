package contradiction

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

// --- NewService ---

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

// --- List ---

// TestService_List_EmptyDBReturnsEmptySlice pins the JSON envelope
// contract: a fresh DB with no contradicts edges must return a
// non-nil, 0-length slice so the downstream envelope emits `[]`
// instead of `null`. Pre-PHASE-2.3 HTTP shell normalized this inline;
// PHASE 2.3 pushes the normalization into the domain Service.
func TestService_List_EmptyDBReturnsEmptySlice(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)

	pairs, err := svc.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if pairs == nil {
		t.Fatal("want non-nil empty slice on empty DB, got nil")
	}
	if len(pairs) != 0 {
		t.Errorf("want 0 pairs, got %d", len(pairs))
	}
}

// TestService_List_FiltersByEntityID exercises both the entityID=""
// (all-pairs) and entityID="x" (filtered) branches. Seeds one
// contradicts pair (a,b) and one unrelated related_to pair (c,d),
// then asserts List with entityID="a" returns exactly the one pair
// that includes "a" while List with entityID="" returns exactly
// one contradicts pair globally (the unrelated pair is filtered by
// relation_type).
func TestService_List_FiltersByEntityID(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	seedEntities(t, db, "a", "b", "c", "d")
	seedEdge(t, db, "a", "b", "contradicts")
	seedEdge(t, db, "c", "d", "related_to")
	svc := New(db)

	pairsFiltered, err := svc.List(t.Context(), "a")
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(pairsFiltered) != 1 {
		t.Fatalf("want 1 pair matching entityID='a', got %d", len(pairsFiltered))
	}
	p := pairsFiltered[0]
	if p.SourceID != "a" && p.TargetID != "a" {
		t.Errorf("pair %+v does not include entityID 'a'", p)
	}

	pairsAll, err := svc.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(pairsAll) != 1 {
		t.Fatalf("want 1 contradicts pair globally, got %d", len(pairsAll))
	}
	if pairsAll[0].SourceID != "a" || pairsAll[0].TargetID != "b" {
		t.Errorf("global pair mismatch: %+v", pairsAll[0])
	}
}

// TestService_List_EmptyEntityIDReturnsAll verifies the "no filter"
// contract separately from the entity-present case, so a future
// refactor that breaks the empty-vs-nonempty merge doesn't silently
// regress one of them.
func TestService_List_EmptyEntityIDReturnsAll(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	seedEntities(t, db, "x", "y")
	seedEdge(t, db, "x", "y", "contradicts")
	svc := New(db)

	pairs, err := svc.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("want 1 contradicts pair, got %d", len(pairs))
	}
}

// TestService_List_PropagatesDBError confirms the domain Service
// surfaces SQL/store errors verbatim (with the prefix added by
// store.GetContradictions) so the HTTP shell can wrap to 500.
func TestService_List_PropagatesDBError(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	// Close DB *before* calling List so the underlying *sql.DB returns
	// "sql: database is closed" on first Query call.
	_ = db.Close()
	svc := New(db)

	_, err = svc.List(t.Context(), "")
	if err == nil {
		t.Fatal("expected closed-DB error, got nil")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("err=%v does not advertise closed-DB context", err)
	}
}

// --- helpers ---

func seedEntities(t *testing.T, db *sql.DB, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := db.Exec(
			`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`,
			id, "world", "seed for "+id,
		); err != nil {
			t.Fatalf("seed entity %s: %v", id, err)
		}
	}
}

func seedEdge(t *testing.T, db *sql.DB, src, dst, rel string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, 1.0)`,
		src, dst, rel,
	); err != nil {
		t.Fatalf("seed edge %s-%s(%s): %v", src, dst, rel, err)
	}
}
