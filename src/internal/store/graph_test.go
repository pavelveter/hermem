package store

import (
	"database/sql"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// --- GetContradictions ---

func TestGetContradictions_NoEdgesReturnsEmpty(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "alpha")
	seedEntity(t, db, "b", "world", "beta")
	got, err := GetContradictions(db, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestGetContradictions_FindsPair(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "alpha")
	seedEntity(t, db, "b", "world", "beta")
	seedEdge(t, db, "a", "b", "contradicts", 1)

	got, err := GetContradictions(db, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 pair, got %d: %v", len(got), got)
	}
	if got[0].SourceID != "a" || got[0].TargetID != "b" {
		t.Fatalf("pair shape: %+v", got[0])
	}
	if got[0].SourceContent != "alpha" || got[0].TargetContent != "beta" {
		t.Fatalf("pair contents: %+v", got[0])
	}
}

func TestGetContradictions_FilterByEntityID(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "alpha")
	seedEntity(t, db, "b", "world", "beta")
	seedEntity(t, db, "c", "world", "gamma")
	seedEdge(t, db, "a", "b", "contradicts", 1)
	seedEdge(t, db, "a", "c", "contradicts", 1)

	// Filter to "b": should return only the pair that involves b (a,b) — not (a,c).
	got, err := GetContradictions(db, "b")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].TargetID != "b" {
		t.Fatalf("filter b: want 1 pair ending at b, got %v", got)
	}

	// Filter to "c": only (a,c) pair.
	got, err = GetContradictions(db, "c")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].TargetID != "c" {
		t.Fatalf("filter c: want 1 pair ending at c, got %v", got)
	}
}

func TestGetContradictions_ExcludesArchived(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "alpha")
	seedEntity(t, db, "b", "world", "beta")
	seedEdge(t, db, "a", "b", "contradicts", 1)
	_, _ = db.Exec(`UPDATE entities SET archived = 1 WHERE id = 'a'`)

	got, err := GetContradictions(db, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("archived source should be filtered: got %v", got)
	}
}

func TestGetContradictions_IgnoresNonContradictsEdges(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "alpha")
	seedEntity(t, db, "b", "world", "beta")
	seedEdge(t, db, "a", "b", "uses", 1) // not 'contradicts'

	got, err := GetContradictions(db, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("non-contradicts edge should be ignored: got %v", got)
	}
}

// --- GetEntitiesByProvenance ---

func TestGetEntitiesByProvenance_RequiresAtLeastOneFilter(t *testing.T) {
	db := openTestDB(t)
	if _, err := GetEntitiesByProvenance(db, "", "", "", 10); err == nil {
		t.Fatal("expected error when all filters empty")
	}
}

func TestGetEntitiesByProvenance_ConversationFilter(t *testing.T) {
	db := openTestDB(t)
	insertProvenanceEntity(t, db, "e1", "conv-1", "", "src-a", "chat")
	insertProvenanceEntity(t, db, "e2", "conv-2", "", "src-a", "chat")

	got, err := GetEntitiesByProvenance(db, "conv-1", "", "", 50)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].ID != "e1" {
		t.Fatalf("want just e1, got %v", ids(got))
	}
}

func TestGetEntitiesByProvenance_MessageFilter(t *testing.T) {
	db := openTestDB(t)
	insertProvenanceEntity(t, db, "e1", "c", "msg-1", "", "chat")
	insertProvenanceEntity(t, db, "e2", "c", "msg-2", "", "chat")

	got, err := GetEntitiesByProvenance(db, "", "msg-2", "", 50)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].ID != "e2" {
		t.Fatalf("want msg-2 entity, got %v", ids(got))
	}
}

func TestGetEntitiesByProvenance_SourceFilter(t *testing.T) {
	db := openTestDB(t)
	insertProvenanceEntity(t, db, "e1", "", "", "users/me", "chat")
	insertProvenanceEntity(t, db, "e2", "", "", "users/you", "chat")

	got, err := GetEntitiesByProvenance(db, "", "", "users/me", 50)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].ID != "e1" {
		t.Fatalf("want users/me entity, got %v", ids(got))
	}
}

func TestGetEntitiesByProvenance_LimitClamping(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 60; i++ {
		insertProvenanceEntity(t, db, idp(i), "c", "", "src", "chat")
	}
	// limit=0 → default 50
	got, err := GetEntitiesByProvenance(db, "c", "", "", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("limit=0 should clamp to default 50, got %d", len(got))
	}
	// limit=99999 → clamp to 200
	got, err = GetEntitiesByProvenance(db, "c", "", "", 99999)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 60 {
		t.Fatalf("only 60 entities exist; should not artificially clamp below available: got %d", len(got))
	}
}

func TestGetEntitiesByProvenance_ExcludesArchived(t *testing.T) {
	db := openTestDB(t)
	insertProvenanceEntity(t, db, "e1", "conv-1", "", "s", "chat")
	insertProvenanceEntity(t, db, "e2", "conv-1", "", "s", "chat")
	_, _ = db.Exec(`UPDATE entities SET archived = 1 WHERE id = 'e2'`)
	got, err := GetEntitiesByProvenance(db, "conv-1", "", "", 50)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].ID != "e1" {
		t.Fatalf("archived should be excluded: got %v", ids(got))
	}
}

// --- FindConnectedComponents ---

func TestFindConnectedComponents_MinSizeFiltersSmall(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "b", "world", "b")
	seedEntity(t, db, "c", "world", "c")
	seedEdge(t, db, "a", "b", "uses", 1)
	seedEdge(t, db, "b", "c", "uses", 1)

	// minSize=2 keeps {a,b,c} (size 3)
	got, err := FindConnectedComponents(db, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("minSize=2, connected a-b-c: want 1 component, got %d", len(got))
	}
	if got[0].Size != 3 {
		t.Fatalf("Size: want 3, got %d", got[0].Size)
	}

	// minSize=4 drops all
	got, _ = FindConnectedComponents(db, 4)
	if len(got) != 0 {
		t.Fatalf("minSize=4 should drop all: got %d", len(got))
	}
}

func TestFindConnectedComponents_TwoComponents(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "b", "world", "b")
	seedEntity(t, db, "x", "world", "x")
	seedEntity(t, db, "y", "world", "y")
	seedEdge(t, db, "a", "b", "uses", 1)
	seedEdge(t, db, "x", "y", "uses", 1)

	got, err := FindConnectedComponents(db, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 components, got %d", len(got))
	}
	// Sorted descending by size — both have size 2 so order is unstable; just check sizes.
	sizes := []int{got[0].Size, got[1].Size}
	sort.Ints(sizes)
	if sizes[0] != 2 || sizes[1] != 2 {
		t.Fatalf("sizes: %v", sizes)
	}
}

func TestFindConnectedComponents_MissingEdgeDrivesOrphan(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "b", "world", "b")
	seedEntity(t, db, "solitary", "world", "no edges")
	seedEdge(t, db, "a", "b", "uses", 1)

	got, err := FindConnectedComponents(db, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Size != 2 {
		t.Fatalf("want just {a,b} component, got %v", got)
	}
}

func TestFindConnectedComponents_AvgDegreeCorrect(t *testing.T) {
	db := openTestDB(t)
	// Star: hub connected to 3 leaves via 3 edges. The adj map is treated as
	// undirected so len(adj[hub]) = 3 and len(adj[leaf]) = 1.
	// totalDegree = 3 + 1 + 1 + 1 = 6, avg = 6 / 4 = 1.5.
	seedEntity(t, db, "hub", "world", "h")
	seedEntity(t, db, "l1", "world", "1")
	seedEntity(t, db, "l2", "world", "2")
	seedEntity(t, db, "l3", "world", "3")
	seedEdge(t, db, "hub", "l1", "uses", 1)
	seedEdge(t, db, "hub", "l2", "uses", 1)
	seedEdge(t, db, "hub", "l3", "uses", 1)

	got, err := FindConnectedComponents(db, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 component, got %d", len(got))
	}
	if got[0].Size != 4 {
		t.Fatalf("Size: %d", got[0].Size)
	}
	want := float64(6) / 4
	if got[0].AvgDegree != want {
		t.Fatalf("AvgDegree: want %v, got %v", want, got[0].AvgDegree)
	}
}

// --- helpers ---

func insertProvenanceEntity(t *testing.T, db *sql.DB, id, convID, msgID, source, srcType string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO entities (id, category, content, archived, conversation_id, message_id, source, source_type, confidence, created_at) VALUES (?, 'world', ?, 0, ?, ?, ?, ?, 0.9, ?)`,
		id, "content-"+id, convID, msgID, source, srcType, time.Now(),
	)
	if err != nil {
		t.Fatalf("insert provenance entity: %v", err)
	}
}

func ids(entities []core.Entity) []string {
	out := make([]string, len(entities))
	for i, e := range entities {
		out[i] = e.ID
	}
	return out
}

func idp(i int) string {
	// Stringify index for batch entity IDs.
	if i < 0 {
		return strings.ReplaceAll("neg", "", "")
	}
	return "e" + itoaSmall(i)
}

func itoaSmall(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
