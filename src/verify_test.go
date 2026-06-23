package main

import (
	"strings"
	"testing"
)

func TestVerifyReportPassEmpty(t *testing.T) {
	r := &VerifyReport{}
	if !r.Pass() {
		t.Error("empty report should pass")
	}
	if !strings.Contains(r.String(), "Status: OK") {
		t.Errorf("empty report should show Status: OK, got:\n%s", r.String())
	}
}

func TestVerifyReportFailWhenIssues(t *testing.T) {
	r := &VerifyReport{CorruptBlobs: 3}
	if r.Pass() {
		t.Error("report with corrupt blobs should fail")
	}
	out := r.String()
	if !strings.Contains(out, "Status: FAIL") {
		t.Errorf("report should show FAIL, got:\n%s", out)
	}
	if !strings.Contains(out, "3 issue(s)") {
		t.Errorf("report should show issue count, got:\n%s", out)
	}
}

func TestVerifyGraphCleanDB(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
		ID: "a", Category: "world", Content: "hello", Embedding: []float32{1, 0, 0, 0},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	schema := defaultSchemaConfig(false)
	r, err := VerifyGraph(db, schema, 4)
	if err != nil {
		t.Fatalf("VerifyGraph: %v", err)
	}
	if !r.Pass() {
		t.Errorf("clean db should pass, report:\n%s", r.String())
	}
	if r.Entities != 1 {
		t.Errorf("Entities = %d, want 1", r.Entities)
	}
	if r.Embeddings != 1 {
		t.Errorf("Embeddings = %d, want 1", r.Embeddings)
	}
}

func TestVerifyGraphOrphanEdge(t *testing.T) {
	db, _ := memDB(t)
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('real', 'world', 'exists')`); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	// With FK ON this INSERT fails at the engine level — but we check the
	// pre-FK state where the edge could exist. For this test we disable
	// FK temporarily via PRAGMA since we want the verify command to
	// detect this as a real condition.
	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('real', 'ghost', 'related_to')`); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}

	schema := defaultSchemaConfig(false)
	r, err := VerifyGraph(db, schema, 768)
	if err != nil {
		t.Fatalf("VerifyGraph: %v", err)
	}
	if r.OrphanEdges < 1 {
		t.Errorf("OrphanEdges = %d, want >= 1", r.OrphanEdges)
	}
	if r.Pass() {
		t.Error("db with orphan edge should fail verify")
	}
}

func TestVerifyGraphCorruptBlob(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	dim := 4
	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
		ID: "good", Category: "world", Content: "fine", Embedding: []float32{1, 0, 0, 0},
	}); err != nil {
		t.Fatalf("store good: %v", err)
	}

	// Manually insert a corrupt blob — wrong byte length.
	if _, err := db.Exec(`INSERT INTO entities (id, category, content, embedding) VALUES ('bad', 'world', 'bad', x'deadbeef')`); err != nil {
		t.Fatalf("insert corrupt: %v", err)
	}

	schema := defaultSchemaConfig(false)
	r, err := VerifyGraph(db, schema, dim)
	if err != nil {
		t.Fatalf("VerifyGraph: %v", err)
	}
	if r.CorruptBlobs < 1 {
		t.Errorf("CorruptBlobs = %d, want >= 1", r.CorruptBlobs)
	}
}

func TestVerifyGraphInvalidStatus(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	schema := taskSchema()
	if err := StoreEntityWithEmbedding(db, vi, schema, Entity{
		ID: "task-1", Category: "task", Content: "do stuff", Embedding: []float32{1, 0, 0, 0},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	// Manually set an invalid status.
	if _, err := db.Exec(`UPDATE entities SET status = 'bogus' WHERE id = 'task-1'`); err != nil {
		t.Fatalf("set status: %v", err)
	}

	r, err := VerifyGraph(db, schema, 4)
	if err != nil {
		t.Fatalf("VerifyGraph: %v", err)
	}
	if r.InvalidStatus < 1 {
		t.Errorf("InvalidStatus = %d, want >= 1", r.InvalidStatus)
	}
}

func TestVerifyGraphInvalidRelation(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
		ID: "a", Category: "world", Content: "a", Embedding: []float32{1, 0, 0, 0},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	// Disable FK for the insert since we want the verify to catch the unknown relation.
	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("pragma off: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('a', 'a', 'bogus_rel')`); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma on: %v", err)
	}

	schema := defaultSchemaConfig(false)
	r, err := VerifyGraph(db, schema, 4)
	if err != nil {
		t.Fatalf("VerifyGraph: %v", err)
	}
	if r.InvalidRelType < 1 {
		t.Errorf("InvalidRelType = %d, want >= 1", r.InvalidRelType)
	}
}

func TestVerifyReportFormatting(t *testing.T) {
	r := &VerifyReport{
		Entities:       100,
		Edges:          50,
		Archived:       10,
		Embeddings:     90,
		CorruptBlobs:   0,
		OrphanEdges:    0,
		InvalidStatus:  0,
		InvalidRelType: 0,
	}
	out := r.String()
	for _, want := range []string{
		"Graph integrity report",
		"entities:",
		"edges:",
		"archived entities:",
		"embeddings:",
		"corrupt embedding blobs:",
		"orphan edges:",
		"invalid status values:",
		"invalid relation types:",
		"Status: OK",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in report:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "0  OK") {
		t.Error("OK check lines should show '0  OK'")
	}
}
