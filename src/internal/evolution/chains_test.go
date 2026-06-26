package evolution

import (
	"context"
	"testing"
)

func TestTraceRevisions_SingleBelief(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status) VALUES (1, 'root', 1.0, 'Active')`)
	if err != nil {
		t.Fatalf("insert root: %v", err)
	}

	nodes, err := TraceRevisions(ctx, db, 1)
	if err != nil {
		t.Fatalf("TraceRevisions: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != 1 {
		t.Errorf("expected ID=1, got %d", nodes[0].ID)
	}
}

func TestTraceRevisions_Chain(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status, parent_chain_id) VALUES (1, 'v1', 1.0, 'Active', NULL)`); err != nil {
		t.Fatalf("insert v1: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status, parent_chain_id) VALUES (2, 'v2', 0.8, 'Active', 1)`); err != nil {
		t.Fatalf("insert v2: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status, parent_chain_id) VALUES (3, 'v3', 0.6, 'Superseded', 2)`); err != nil {
		t.Fatalf("insert v3: %v", err)
	}

	nodes, err := TraceRevisions(ctx, db, 3)
	if err != nil {
		t.Fatalf("TraceRevisions: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	if nodes[0].ID != 1 {
		t.Errorf("expected first node ID=1, got %d", nodes[0].ID)
	}
	if nodes[2].ID != 3 {
		t.Errorf("expected last node ID=3, got %d", nodes[2].ID)
	}
}

func TestTraceRevisions_InvalidID(t *testing.T) {
	_, err := TraceRevisions(context.Background(), nil, 0)
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}
