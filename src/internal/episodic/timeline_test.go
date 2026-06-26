package episodic

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openTimelineTestDB returns an in-memory SQLite with all four
// tables needed for ReconstructTimeline: episodes, entities,
// events, episode_memories, episode_tasks.
func openTimelineTestDB(t *testing.T) *sql.DB {
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
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
			type TEXT NOT NULL CHECK(type IN ('message', 'action', 'observation', 'system')),
			content TEXT NOT NULL DEFAULT '',
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			metadata TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS episode_memories (
			episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
			entity_id TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
			role TEXT NOT NULL DEFAULT 'extracted',
			linked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (episode_id, entity_id, role)
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

func TestTimelineService_ReconstructTimeline_RequiresEpisodeID(t *testing.T) {
	db := openTimelineTestDB(t)
	svc := NewTimelineService(db)
	_, err := svc.ReconstructTimeline(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestTimelineService_ReconstructTimeline_EmptyEpisodeReturnsEmptySlice(t *testing.T) {
	db := openTimelineTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-empty')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	svc := NewTimelineService(db)
	got, err := svc.ReconstructTimeline(context.Background(), "ep-empty")
	if err != nil {
		t.Fatalf("ReconstructTimeline: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 entries, got %d", len(got))
	}
}

func TestTimelineService_ReconstructTimeline_OnlyEvents(t *testing.T) {
	db := openTimelineTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	svc := NewTimelineService(db)
	evSvc := NewEventService(db)

	for i, e := range []Event{
		{ID: "e1", EpisodeID: "ep-1", Type: EventMessage, Content: "hello", Timestamp: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)},
		{ID: "e2", EpisodeID: "ep-1", Type: EventAction, Content: "clicked", Timestamp: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)},
	} {
		if err := evSvc.CreateEvent(context.Background(), e); err != nil {
			t.Fatalf("CreateEvent %d: %v", i, err)
		}
	}
	got, err := svc.ReconstructTimeline(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("ReconstructTimeline: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	for _, e := range got {
		if e.Kind != TimelineEvent {
			t.Errorf("entry %s: want kind=event, got %s", e.SourceID, e.Kind)
		}
	}
	if got[0].SourceID != "e1" || got[1].SourceID != "e2" {
		t.Fatalf("order: want [e1, e2], got [%s, %s]", got[0].SourceID, got[1].SourceID)
	}
}

func TestTimelineService_ReconstructTimeline_MergesEventsMemoriesAndTasks(t *testing.T) {
	db := openTimelineTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'a memory')`); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content, status) VALUES ('t1', 'task', 'a task', 'pending')`); err != nil {
		t.Fatalf("seed task entity: %v", err)
	}
	evSvc := NewEventService(db)
	if err := evSvc.CreateEvent(context.Background(), Event{
		ID: "e1", EpisodeID: "ep-1", Type: EventMessage, Content: "event",
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	linkSvc := NewLinkService(db)
	if err := linkSvc.LinkMemory(context.Background(), "ep-1", "m1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	taskSvc := NewTaskLinkService(db)
	if err := taskSvc.LinkTask(context.Background(), "ep-1", "t1"); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	svc := NewTimelineService(db)
	got, err := svc.ReconstructTimeline(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("ReconstructTimeline: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries (event + memory + task), got %d", len(got))
	}
	kindCounts := map[TimelineEntryKind]int{}
	for _, e := range got {
		kindCounts[e.Kind]++
	}
	if kindCounts[TimelineEvent] != 1 {
		t.Errorf("event count: want 1, got %d", kindCounts[TimelineEvent])
	}
	if kindCounts[TimelineMemory] != 1 {
		t.Errorf("memory count: want 1, got %d", kindCounts[TimelineMemory])
	}
	if kindCounts[TimelineTask] != 1 {
		t.Errorf("task count: want 1, got %d", kindCounts[TimelineTask])
	}
}

func TestTimelineService_ReconstructTimeline_OrdersByTimestamp(t *testing.T) {
	db := openTimelineTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'mem')`); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content, status) VALUES ('t1', 'task', 'task', 'pending')`); err != nil {
		t.Fatalf("seed task entity: %v", err)
	}
	evSvc := NewEventService(db)
	// Out-of-order timestamps.
	if err := evSvc.CreateEvent(context.Background(), Event{
		ID: "e-late", EpisodeID: "ep-1", Type: EventMessage, Content: "late",
		Timestamp: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if err := evSvc.CreateEvent(context.Background(), Event{
		ID: "e-early", EpisodeID: "ep-1", Type: EventMessage, Content: "early",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	linkSvc := NewLinkService(db)
	if err := linkSvc.LinkMemory(context.Background(), "ep-1", "m1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	taskSvc := NewTaskLinkService(db)
	if err := taskSvc.LinkTask(context.Background(), "ep-1", "t1"); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	svc := NewTimelineService(db)
	got, err := svc.ReconstructTimeline(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("ReconstructTimeline: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 entries, got %d", len(got))
	}
	// Find the e-early event — it must come before e-late.
	var earlyIdx, lateIdx = -1, -1
	for i, e := range got {
		if e.SourceID == "e-early" {
			earlyIdx = i
		}
		if e.SourceID == "e-late" {
			lateIdx = i
		}
	}
	if earlyIdx == -1 || lateIdx == -1 {
		t.Fatalf("missing events; got %v", got)
	}
	if earlyIdx >= lateIdx {
		t.Errorf("e-early should precede e-late: early=%d late=%d", earlyIdx, lateIdx)
	}
}

func TestTimelineService_ReconstructTimeline_StableOnTiebreak(t *testing.T) {
	db := openTimelineTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	evSvc := NewEventService(db)
	sameTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"e-c", "e-a", "e-b"} {
		if err := evSvc.CreateEvent(context.Background(), Event{
			ID: id, EpisodeID: "ep-1", Type: EventMessage, Content: id,
			Timestamp: sameTime,
		}); err != nil {
			t.Fatalf("CreateEvent %s: %v", id, err)
		}
	}
	svc := NewTimelineService(db)
	got, err := svc.ReconstructTimeline(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("ReconstructTimeline: %v", err)
	}
	// All share the same timestamp; sort falls back to entryLess
	// (kind then source_id). All are TimelineEvent here so they
	// tiebreak lexicographically: e-a, e-b, e-c.
	wantIDs := []string{"e-a", "e-b", "e-c"}
	for i, e := range got {
		if e.SourceID != wantIDs[i] {
			t.Errorf("position %d: want %s, got %s", i, wantIDs[i], e.SourceID)
		}
	}
}

func TestTimelineService_ReconstructTimeline_EventTypePropagatesToEntry(t *testing.T) {
	db := openTimelineTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	evSvc := NewEventService(db)
	if err := evSvc.CreateEvent(context.Background(), Event{
		ID: "e1", EpisodeID: "ep-1", Type: EventAction, Content: "clicked",
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	svc := NewTimelineService(db)
	got, _ := svc.ReconstructTimeline(context.Background(), "ep-1")
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0].Type != string(EventAction) {
		t.Errorf("entry type: want 'action', got %q", got[0].Type)
	}
}

func TestTimelineService_ReconstructTimeline_MemoryRolePropagatesToEntry(t *testing.T) {
	db := openTimelineTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'mem')`); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	linkSvc := NewLinkService(db)
	if err := linkSvc.LinkMemory(context.Background(), "ep-1", "m1", "referenced"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	svc := NewTimelineService(db)
	got, _ := svc.ReconstructTimeline(context.Background(), "ep-1")
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0].Type != "referenced" {
		t.Errorf("entry type (memory role): want 'referenced', got %q", got[0].Type)
	}
}

func TestTimelineService_ReconstructTimeline_TaskStatusPropagatesToEntry(t *testing.T) {
	db := openTimelineTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content, status) VALUES ('t1', 'task', 'task', 'completed')`); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	taskSvc := NewTaskLinkService(db)
	if err := taskSvc.LinkTask(context.Background(), "ep-1", "t1"); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	svc := NewTimelineService(db)
	got, _ := svc.ReconstructTimeline(context.Background(), "ep-1")
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0].Type != "completed" {
		t.Errorf("entry type (task status): want 'completed', got %q", got[0].Type)
	}
}
