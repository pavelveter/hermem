package evolution

import (
	"testing"
)

func TestListActiveBeliefs_ExcludesSuperseded(t *testing.T) {
	db := openDB(t)
	ctx := t.Context()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status) VALUES (1, 'active', 1.0, 'Active')`); err != nil {
		t.Fatalf("insert active: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status) VALUES (2, 'superseded', 0.5, 'Superseded')`); err != nil {
		t.Fatalf("insert superseded: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status) VALUES (3, 'archived', 0.3, 'Archived')`); err != nil {
		t.Fatalf("insert archived: %v", err)
	}

	active, err := ListActiveBeliefs(ctx, db, false)
	if err != nil {
		t.Fatalf("ListActiveBeliefs: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active belief, got %d", len(active))
	}
	if active[0].ID != 1 {
		t.Errorf("expected ID=1, got %d", active[0].ID)
	}
}

func TestListActiveBeliefs_IncludeSuperseded(t *testing.T) {
	db := openDB(t)
	ctx := t.Context()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status) VALUES (1, 'active', 1.0, 'Active')`); err != nil {
		t.Fatalf("insert active: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status) VALUES (2, 'superseded', 0.5, 'Superseded')`); err != nil {
		t.Fatalf("insert superseded: %v", err)
	}

	all, err := ListActiveBeliefs(ctx, db, true)
	if err != nil {
		t.Fatalf("ListActiveBeliefs(includeSuperseded): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 beliefs, got %d", len(all))
	}
}
