package store

import (
	"context"
	"testing"
)

func TestVerifyOrphanEdges_NoOrphans(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create two entities and an edge between them
	db.Exec(`INSERT INTO entities (id, category, content) VALUES ('a', 'world', 'alpha')`)
	db.Exec(`INSERT INTO entities (id, category, content) VALUES ('b', 'world', 'beta')`)
	db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES ('a', 'b', 'related_to', 1.0)`)

	edges, err := VerifyOrphanEdges(ctx, db)
	if err != nil {
		t.Fatalf("VerifyOrphanEdges: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 orphan edges, got %d", len(edges))
	}
}

func TestVerifyOrphanEdges_WithOrphans(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create one entity
	db.Exec(`INSERT INTO entities (id, category, content) VALUES ('a', 'world', 'alpha')`)

	// Temporarily disable foreign keys to insert an orphan edge
	db.Exec(`PRAGMA foreign_keys = OFF`)
	db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES ('a', 'missing', 'related_to', 1.0)`)
	db.Exec(`PRAGMA foreign_keys = ON`)

	edges, err := VerifyOrphanEdges(ctx, db)
	if err != nil {
		t.Fatalf("VerifyOrphanEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("expected 1 orphan edge, got %d", len(edges))
	}
	if len(edges) > 0 && edges[0].Source != "a" {
		t.Errorf("expected source 'a', got %q", edges[0].Source)
	}
	if len(edges) > 0 && edges[0].Target != "missing" {
		t.Errorf("expected target 'missing', got %q", edges[0].Target)
	}
}

func TestVerifyDimensionMismatches_NoMismatches(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create entity with correct embedding (3 dims = 12 bytes)
	db.Exec(`INSERT INTO entities (id, category, content, embedding) VALUES ('a', 'world', 'alpha', ?)`,
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	mismatches, err := VerifyDimensionMismatches(ctx, db, 3)
	if err != nil {
		t.Fatalf("VerifyDimensionMismatches: %v", err)
	}
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches, got %d", len(mismatches))
	}
}

func TestVerifyDimensionMismatches_WithMismatches(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create entity with wrong embedding (2 dims = 8 bytes, expected 3 dims = 12 bytes)
	db.Exec(`INSERT INTO entities (id, category, content, embedding) VALUES ('a', 'world', 'alpha', ?)`,
		[]byte{0, 0, 0, 0, 0, 0, 0, 0})

	mismatches, err := VerifyDimensionMismatches(ctx, db, 3)
	if err != nil {
		t.Fatalf("VerifyDimensionMismatches: %v", err)
	}
	if len(mismatches) != 1 {
		t.Errorf("expected 1 mismatch, got %d", len(mismatches))
	}
	if len(mismatches) > 0 && mismatches[0].ID != "a" {
		t.Errorf("expected ID 'a', got %q", mismatches[0].ID)
	}
	if len(mismatches) > 0 && mismatches[0].Bytes != 8 {
		t.Errorf("expected 8 bytes, got %d", mismatches[0].Bytes)
	}
}

func TestVerifyDimensionMismatches_IgnoresArchived(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create archived entity with wrong embedding
	db.Exec(`INSERT INTO entities (id, category, content, embedding, archived) VALUES ('a', 'world', 'alpha', ?, 1)`,
		[]byte{0, 0, 0, 0})

	mismatches, err := VerifyDimensionMismatches(ctx, db, 3)
	if err != nil {
		t.Fatalf("VerifyDimensionMismatches: %v", err)
	}
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches (archived ignored), got %d", len(mismatches))
	}
}

func TestVerifyDimensionMismatches_IgnoresNullEmbedding(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create entity with no embedding
	db.Exec(`INSERT INTO entities (id, category, content) VALUES ('a', 'world', 'alpha')`)

	mismatches, err := VerifyDimensionMismatches(ctx, db, 3)
	if err != nil {
		t.Fatalf("VerifyDimensionMismatches: %v", err)
	}
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches (null embedding ignored), got %d", len(mismatches))
	}
}
