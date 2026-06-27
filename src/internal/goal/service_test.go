// Package goal — service-level tests for GoalService.
package goal

import (
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"

	_ "github.com/mattn/go-sqlite3"
)

// newGoalFixture wires an in-memory SQLite DB with a minimal entities
// table and a Goal Service. The schema has one stateful category ("goal")
// so store.SetStatus + store.ListTasks accept it.
func newGoalFixture(t *testing.T) (*Service, *sql.DB, core.SchemaConfig) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open mem db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY,
		category TEXT DEFAULT '',
		content TEXT DEFAULT '',
		embedding BLOB,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		status TEXT DEFAULT '',
		conversation_id TEXT DEFAULT '',
		message_id TEXT DEFAULT '',
		source TEXT DEFAULT '',
		source_type TEXT DEFAULT '',
		created_at DATETIME,
		confidence REAL,
		priority INTEGER DEFAULT 0,
		archived INTEGER DEFAULT 0
	)`); err != nil {
		db.Close()
		t.Fatalf("create entities table: %v", err)
	}
	schema := core.SchemaConfig{
		StatefulCategories: map[string]bool{"goal": true},
		ValidStates:        map[string]bool{"pending": true, "running": true, "completed": true},
		ValidStateOrder:    []string{"pending", "running", "completed"},
		AllowedCategories:  map[string]bool{"goal": true, "world": true},
		StatefulEnabled:    true,
	}
	return New(db), db, schema
}

// seedGoal inserts one goal row with the given status.
func seedGoal(t *testing.T, db *sql.DB, id, content, status string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, status, updated_at)
		 VALUES (?, 'goal', ?, ?, CURRENT_TIMESTAMP)`,
		id, content, status,
	); err != nil {
		t.Fatalf("seed goal: %v", err)
	}
}

func TestNewService_Success(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

func TestService_Status_RejectsEmptyFields(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	svc := New(db)
	schema := core.SchemaConfig{StatefulEnabled: true}
	if err := svc.Status(t.Context(), "", "running", schema); err == nil {
		t.Fatal("expected error for empty id")
	}
	if err := svc.Status(t.Context(), "g1", "", schema); err == nil {
		t.Fatal("expected error for empty status")
	}
}

func TestService_Status_Transitions(t *testing.T) {
	svc, db, schema := newGoalFixture(t)
	defer db.Close()
	seedGoal(t, db, "g1", "write tests", "pending")
	if err := svc.Status(t.Context(), "g1", "running", schema); err != nil {
		t.Fatalf("Status pending→running: %v", err)
	}
}

func TestService_Status_NotFound(t *testing.T) {
	svc, db, schema := newGoalFixture(t)
	defer db.Close()
	err := svc.Status(t.Context(), "no-such-goal", "running", schema)
	if err == nil {
		t.Fatal("expected error for nonexistent goal")
	}
}

func TestService_List_EmptyDBReturnsEmptySlice(t *testing.T) {
	svc, db, schema := newGoalFixture(t)
	defer db.Close()
	goals, err := svc.List(t.Context(), "", "", schema)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if goals == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(goals) != 0 {
		t.Fatalf("expected 0 goals, got %d", len(goals))
	}
}

func TestService_List_ReturnsFiltered(t *testing.T) {
	svc, db, schema := newGoalFixture(t)
	defer db.Close()
	seedGoal(t, db, "g1", "goal one", "pending")
	seedGoal(t, db, "g2", "goal two", "running")

	goals, err := svc.List(t.Context(), "pending", "", schema)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("expected 1 pending goal, got %d", len(goals))
	}
	if goals[0].ID != "g1" {
		t.Fatalf("expected g1, got %s", goals[0].ID)
	}
}

func TestService_Get_RejectsEmptyID(t *testing.T) {
	svc, db, schema := newGoalFixture(t)
	defer db.Close()
	_, err := svc.Get(t.Context(), "", schema)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestService_Get_NotFound(t *testing.T) {
	svc, db, schema := newGoalFixture(t)
	defer db.Close()
	_, err := svc.Get(t.Context(), "no-such-goal", schema)
	if err == nil {
		t.Fatal("expected error for nonexistent goal")
	}
}

func TestService_Get_ReturnsGoal(t *testing.T) {
	svc, db, schema := newGoalFixture(t)
	defer db.Close()
	seedGoal(t, db, "g1", "learn Go", "pending")
	e, err := svc.Get(t.Context(), "g1", schema)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e.ID != "g1" {
		t.Fatalf("expected g1, got %s", e.ID)
	}
	if e.Content != "learn Go" {
		t.Fatalf("expected 'learn Go', got %s", e.Content)
	}
}
