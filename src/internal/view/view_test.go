package view

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

func seedEntity(t *testing.T, db *sql.DB, id, category, content string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`, id, category, content); err != nil {
		t.Fatalf("seed entity %s: %v", id, err)
	}
}

func seedEdge(t *testing.T, db *sql.DB, src, dst, rel string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES (?, ?, ?)`, src, dst, rel); err != nil {
		t.Fatalf("seed edge %s->%s: %v", src, dst, err)
	}
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("mem db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSnapshot_EmptyDB checks the empty case stays JSON-friendly: empty
// slices (not null) and zero totals, so the page renders "0 nodes".
func TestSnapshot_EmptyDB(t *testing.T) {
	g, err := Snapshot(context.Background(), openDB(t), 0)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Fatalf("want empty graph, got %d nodes / %d edges", len(g.Nodes), len(g.Edges))
	}
	if g.Truncated {
		t.Error("empty db must not report truncation")
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(wire["nodes"]) != "[]" || string(wire["edges"]) != "[]" {
		t.Fatalf("want [] for empty slices, got nodes=%s edges=%s", wire["nodes"], wire["edges"])
	}
}

// TestSnapshot_ResolvesEdgesToVisibleNodes covers the truncation contract:
// the cap keeps highest-degree nodes and drops edges whose other endpoint
// fell outside it, so the page never draws a dangling link.
func TestSnapshot_ResolvesEdgesToVisibleNodes(t *testing.T) {
	db := openDB(t)
	// hub has two edges; leaf-a and leaf-b one each; orphan none.
	seedEntity(t, db, "hub", "world", "hub node")
	seedEntity(t, db, "leaf-a", "opinion", "leaf a")
	seedEntity(t, db, "leaf-b", "task", "leaf b")
	seedEntity(t, db, "orphan", "observation", "orphan node")
	seedEdge(t, db, "hub", "leaf-a", "related_to")
	seedEdge(t, db, "hub", "leaf-b", "part_of")

	g, err := Snapshot(context.Background(), db, 2)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d: %+v", len(g.Nodes), g.Nodes)
	}
	if g.Nodes[0].ID != "hub" {
		t.Fatalf("highest degree first: want hub, got %q", g.Nodes[0].ID)
	}
	if !g.Truncated {
		t.Error("4 nodes capped to 2 must report truncation")
	}
	if g.TotalNodes != 4 {
		t.Errorf("total_nodes: want 4, got %d", g.TotalNodes)
	}
	if g.TotalEdges != 2 {
		t.Errorf("total_edges: want 2, got %d", g.TotalEdges)
	}
	for _, e := range g.Edges {
		if e.SourceID != "hub" && e.TargetID != "hub" {
			t.Errorf("edge %v survived without its endpoints in the page", e)
		}
	}
}

// TestSnapshot_ExcludesArchivedEntities — archived rows must never appear,
// matching every other read path in the store.
func TestSnapshot_ExcludesArchivedEntities(t *testing.T) {
	db := openDB(t)
	seedEntity(t, db, "live", "world", "live")
	seedEntity(t, db, "gone", "world", "archived")
	if _, err := db.Exec(`UPDATE entities SET archived = 1 WHERE id = 'gone'`); err != nil {
		t.Fatalf("archive: %v", err)
	}

	g, err := Snapshot(context.Background(), db, 0)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(g.Nodes) != 1 || g.Nodes[0].ID != "live" {
		t.Fatalf("want only the live node, got %+v", g.Nodes)
	}
	if g.TotalNodes != 1 {
		t.Errorf("archived rows must not count toward total_nodes, got %d", g.TotalNodes)
	}
}

// TestSnapshot_ClampsLimitToNodeCap keeps the documented ceiling honest.
func TestSnapshot_ClampsLimitToNodeCap(t *testing.T) {
	db := openDB(t)
	for i := 0; i < NodeCap+5; i++ {
		id := "n" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		if _, err := db.Exec(`INSERT OR IGNORE INTO entities (id, category, content) VALUES (?, 'world', 'x')`, id); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	g, err := Snapshot(context.Background(), db, NodeCap*10)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(g.Nodes) > NodeCap {
		t.Fatalf("limit above NodeCap must clamp to %d, got %d", NodeCap, len(g.Nodes))
	}
}

// TestHandler_ServesPageAndSnapshot pins the two routes the page depends on.
func TestHandler_ServesPageAndSnapshot(t *testing.T) {
	db := openDB(t)
	seedEntity(t, db, "a", "world", "alpha")
	h := Handler(db, 0)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("page: want 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("page content-type: %q", ct)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("page body is empty")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/graph", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("api: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache-control: want no-store, got %q", got)
	}
	var g Graph
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(g.Nodes) != 1 || g.Nodes[0].ID != "a" {
		t.Fatalf("want node a, got %+v", g.Nodes)
	}

	// Exactly two routes exist; anything else must not silently return the page.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/other", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path: want 404, got %d", rec.Code)
	}
}
