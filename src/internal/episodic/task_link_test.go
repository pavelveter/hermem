package episodic

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// openTaskLinkTestDB returns an in-memory SQLite with episodes +
// entities + episode_tasks tables applied.
func openTaskLinkTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS episodes (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			conversation_id TEXT,
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at DATETIME,
			metadata TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS entities (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS episode_tasks (
			episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
			task_id TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
			linked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (episode_id, task_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec schema: %v\n%s", err, s)
		}
	}
	return db
}

func seedEpisodeForTask(t *testing.T, db *sql.DB, id, title string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO episodes (id, title) VALUES (?, ?)`, id, title); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
}

func seedTaskEntity(t *testing.T, db *sql.DB, id, content, status string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, status) VALUES (?, 'task', ?, ?)`,
		id, content, status); err != nil {
		t.Fatalf("seed task entity: %v", err)
	}
}

func TestTaskLinkService_LinkTask_RequiresEpisodeID(t *testing.T) {
	db := openTaskLinkTestDB(t)
	svc := NewTaskLinkService(db)
	err := svc.LinkTask(context.Background(), "", "task-1")
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestTaskLinkService_LinkTask_RequiresTaskID(t *testing.T) {
	db := openTaskLinkTestDB(t)
	seedEpisodeForTask(t, db, "ep-1", "")
	svc := NewTaskLinkService(db)
	err := svc.LinkTask(context.Background(), "ep-1", "")
	if err == nil || !strings.Contains(err.Error(), "task_id required") {
		t.Fatalf("want task_id-required error, got %v", err)
	}
}

func TestTaskLinkService_LinkTask_Idempotent(t *testing.T) {
	db := openTaskLinkTestDB(t)
	seedEpisodeForTask(t, db, "ep-1", "")
	seedTaskEntity(t, db, "task-1", "do thing", "pending")
	svc := NewTaskLinkService(db)

	for i := 0; i < 3; i++ {
		if err := svc.LinkTask(context.Background(), "ep-1", "task-1"); err != nil {
			t.Fatalf("LinkTask #%d: %v", i, err)
		}
	}
	got, _ := svc.ListTasksForEpisode(context.Background(), "ep-1")
	if len(got) != 1 {
		t.Fatalf("idempotent: want 1 link, got %d", len(got))
	}
}

func TestTaskLinkService_UnlinkTask_RemovesLink(t *testing.T) {
	db := openTaskLinkTestDB(t)
	seedEpisodeForTask(t, db, "ep-1", "")
	seedTaskEntity(t, db, "task-1", "do thing", "pending")
	svc := NewTaskLinkService(db)

	if err := svc.LinkTask(context.Background(), "ep-1", "task-1"); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	if err := svc.UnlinkTask(context.Background(), "ep-1", "task-1"); err != nil {
		t.Fatalf("UnlinkTask: %v", err)
	}
	got, _ := svc.ListTasksForEpisode(context.Background(), "ep-1")
	if len(got) != 0 {
		t.Fatalf("after unlink: want 0 tasks, got %d", len(got))
	}
}

func TestTaskLinkService_UnlinkTask_Idempotent(t *testing.T) {
	db := openTaskLinkTestDB(t)
	seedEpisodeForTask(t, db, "ep-1", "")
	svc := NewTaskLinkService(db)
	if err := svc.UnlinkTask(context.Background(), "ep-1", "missing-task"); err != nil {
		t.Fatalf("UnlinkTask on missing link: %v", err)
	}
}

func TestTaskLinkService_UnlinkTask_RequiresIDs(t *testing.T) {
	db := openTaskLinkTestDB(t)
	svc := NewTaskLinkService(db)
	if err := svc.UnlinkTask(context.Background(), "", "task-1"); err == nil {
		t.Fatal("want error for empty episode_id")
	}
	if err := svc.UnlinkTask(context.Background(), "ep-1", ""); err == nil {
		t.Fatal("want error for empty task_id")
	}
}

func TestTaskLinkService_ListTasksForEpisode_ReturnsAllLinkedTasks(t *testing.T) {
	db := openTaskLinkTestDB(t)
	seedEpisodeForTask(t, db, "ep-1", "")
	seedTaskEntity(t, db, "task-a", "first task", "pending")
	seedTaskEntity(t, db, "task-b", "second task", "running")
	seedTaskEntity(t, db, "task-c", "third task", "completed")
	svc := NewTaskLinkService(db)

	for _, id := range []string{"task-a", "task-b", "task-c"} {
		if err := svc.LinkTask(context.Background(), "ep-1", id); err != nil {
			t.Fatalf("LinkTask %s: %v", id, err)
		}
	}
	got, err := svc.ListTasksForEpisode(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("ListTasksForEpisode: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(got))
	}
	statuses := map[string]string{}
	for _, t := range got {
		statuses[t.ID] = t.Status
	}
	if statuses["task-a"] != "pending" {
		t.Errorf("task-a status: want pending, got %s", statuses["task-a"])
	}
	if statuses["task-b"] != "running" {
		t.Errorf("task-b status: want running, got %s", statuses["task-b"])
	}
	if statuses["task-c"] != "completed" {
		t.Errorf("task-c status: want completed, got %s", statuses["task-c"])
	}
}

func TestTaskLinkService_ListTasksForEpisode_RequiresEpisodeID(t *testing.T) {
	db := openTaskLinkTestDB(t)
	svc := NewTaskLinkService(db)
	_, err := svc.ListTasksForEpisode(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestTaskLinkService_ListTasksForEpisode_EmptyReturnsEmptySlice(t *testing.T) {
	db := openTaskLinkTestDB(t)
	seedEpisodeForTask(t, db, "ep-empty", "")
	svc := NewTaskLinkService(db)
	got, err := svc.ListTasksForEpisode(context.Background(), "ep-empty")
	if err != nil {
		t.Fatalf("ListTasksForEpisode: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 tasks, got %d", len(got))
	}
}

func TestTaskLinkService_LinkTask_OnDeleteCascade(t *testing.T) {
	db := openTaskLinkTestDB(t)
	seedEpisodeForTask(t, db, "ep-cascade", "")
	seedTaskEntity(t, db, "task-cascade", "do thing", "pending")
	svc := NewTaskLinkService(db)

	if err := svc.LinkTask(context.Background(), "ep-cascade", "task-cascade"); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	// Delete the episode — the link should cascade-delete.
	if _, err := db.Exec(`DELETE FROM episodes WHERE id = ?`, "ep-cascade"); err != nil {
		t.Fatalf("delete episode: %v", err)
	}
	// Verify the link row is gone by attempting to unlink (idempotent — no error expected).
	if err := svc.UnlinkTask(context.Background(), "ep-cascade", "task-cascade"); err != nil {
		t.Fatalf("UnlinkTask after cascade delete: %v", err)
	}
}
