package episodic

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openPlaybackTestDB returns an in-memory SQLite with all five
// tables needed for playback: episodes + entities + events +
// episode_memories + episode_tasks.
func openPlaybackTestDB(t *testing.T) *sql.DB {
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

func TestPlaybackService_Playback_RequiresEpisodeID(t *testing.T) {
	db := openPlaybackTestDB(t)
	svc := NewPlaybackService(db)
	_, err := svc.Playback(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestPlaybackService_Playback_EmptyEpisodeReturnsEmptySlice(t *testing.T) {
	db := openPlaybackTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-empty')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	svc := NewPlaybackService(db)
	frames, err := svc.Playback(context.Background(), "ep-empty")
	if err != nil {
		t.Fatalf("Playback: %v", err)
	}
	if frames == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(frames) != 0 {
		t.Fatalf("want 0 frames, got %d", len(frames))
	}
}

func TestPlaybackService_Playback_NonExistentEpisodeReturnsEmptyFrames(t *testing.T) {
	// Episodes that don't exist (or have no events/links) are
	// valid cases — ReconstructTimeline returns an empty slice,
	// so Playback does too. No error is propagated because the
	// chronological feed for a non-existent episode is trivially
	// empty. Callers that need to distinguish "episode doesn't
	// exist" from "episode has no frames" should call
	// EpisodeService.GetEpisode first.
	db := openPlaybackTestDB(t)
	svc := NewPlaybackService(db)
	frames, err := svc.Playback(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Playback on non-existent episode: %v", err)
	}
	if frames == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(frames) != 0 {
		t.Fatalf("want 0 frames, got %d", len(frames))
	}
}

func TestPlaybackService_Playback_ActorReflectsKindContext(t *testing.T) {
	db := openPlaybackTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'memory content')`); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content, status) VALUES ('t1', 'task', 'task content', 'completed')`); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := NewEventService(db).CreateEvent(context.Background(), Event{
		ID: "e1", EpisodeID: "ep-1", Type: EventMessage, Content: "hello",
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if err := NewLinkService(db).LinkMemory(context.Background(), "ep-1", "m1", "referenced"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	if err := NewTaskLinkService(db).LinkTask(context.Background(), "ep-1", "t1"); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	svc := NewPlaybackService(db)
	frames, err := svc.Playback(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("Playback: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("want 3 frames, got %d", len(frames))
	}
	// Verify Actor mapping per kind.
	actors := map[string]string{}
	for _, f := range frames {
		actors[f.Type+"-"+f.Source] = f.Actor
	}
	if actors["event-e1"] != "message" {
		t.Errorf("event actor: want 'message', got %q", actors["event-e1"])
	}
	if actors["memory-m1"] != "referenced" {
		t.Errorf("memory actor: want 'referenced', got %q", actors["memory-m1"])
	}
	if actors["task-t1"] != "completed" {
		t.Errorf("task actor: want 'completed', got %q", actors["task-t1"])
	}
}

func TestPlaybackService_Playback_PreservesChronologicalOrder(t *testing.T) {
	db := openPlaybackTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	evSvc := NewEventService(db)
	for i, e := range []Event{
		{ID: "e-late", EpisodeID: "ep-1", Type: EventMessage, Content: "late",
			Timestamp: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)},
		{ID: "e-early", EpisodeID: "ep-1", Type: EventMessage, Content: "early",
			Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "e-mid", EpisodeID: "ep-1", Type: EventMessage, Content: "mid",
			Timestamp: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	} {
		if err := evSvc.CreateEvent(context.Background(), e); err != nil {
			t.Fatalf("CreateEvent %d: %v", i, err)
		}
	}
	svc := NewPlaybackService(db)
	frames, err := svc.Playback(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("Playback: %v", err)
	}
	wantOrder := []string{"e-early", "e-mid", "e-late"}
	for i, f := range frames {
		if f.Source != wantOrder[i] {
			t.Errorf("position %d: want %s, got %s", i, wantOrder[i], f.Source)
		}
	}
}

func TestPlaybackService_ExportJSON_RoundTripsFrames(t *testing.T) {
	db := openPlaybackTestDB(t)
	svc := NewPlaybackService(db)
	frames := []PlaybackFrame{
		{Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), Type: "event", Source: "e1", Actor: "message", Content: "hello"},
		{Timestamp: time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC), Type: "memory", Source: "m1", Actor: "extracted", Content: "fact"},
	}
	out, err := svc.ExportJSON(frames)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var back []PlaybackFrame
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, out)
	}
	if len(back) != 2 {
		t.Fatalf("round-trip: want 2 frames, got %d", len(back))
	}
	if back[0].Source != "e1" || back[1].Source != "m1" {
		t.Fatalf("round-trip order/content mismatch: %+v", back)
	}
	if back[0].Content != "hello" || back[1].Content != "fact" {
		t.Fatalf("round-trip content mismatch: %+v", back)
	}
}

func TestPlaybackService_ExportJSON_NilFramesProducesEmptyArray(t *testing.T) {
	db := openPlaybackTestDB(t)
	svc := NewPlaybackService(db)
	out, err := svc.ExportJSON(nil)
	if err != nil {
		t.Fatalf("ExportJSON(nil): %v", err)
	}
	if string(out) != "[]" {
		t.Fatalf("nil frames: want [] output, got %s", out)
	}
}

func TestPlaybackService_ExportMarkdown_HeaderAndBullets(t *testing.T) {
	db := openPlaybackTestDB(t)
	svc := NewPlaybackService(db)
	frames := []PlaybackFrame{
		{Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), Type: "event", Source: "e1", Actor: "message", Content: "hello"},
		{Timestamp: time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC), Type: "memory", Source: "m1", Actor: "referenced", Content: "a memory"},
	}
	md := svc.ExportMarkdown(frames)
	if !strings.HasPrefix(md, "# Episode Timeline") {
		t.Errorf("markdown must start with header, got: %q", md)
	}
	if !strings.Contains(md, "- `2026-01-01T12:00:00Z` — **message** (event): hello") {
		t.Errorf("markdown missing event line: %q", md)
	}
	if !strings.Contains(md, "**referenced** (memory): a memory") {
		t.Errorf("markdown missing memory line: %q", md)
	}
}

func TestPlaybackService_ExportMarkdown_EmptyFramesHeaderOnly(t *testing.T) {
	db := openPlaybackTestDB(t)
	svc := NewPlaybackService(db)
	md := svc.ExportMarkdown(nil)
	if !strings.HasPrefix(md, "# Episode Timeline") {
		t.Errorf("empty markdown: want header, got %q", md)
	}
	if strings.Contains(md, "**") {
		t.Errorf("empty markdown: no bold bullets expected, got %q", md)
	}
}

func TestPlaybackService_ExportText_OneLinePerFrame(t *testing.T) {
	db := openPlaybackTestDB(t)
	svc := NewPlaybackService(db)
	frames := []PlaybackFrame{
		{Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), Type: "event", Source: "e1", Actor: "message", Content: "hello"},
	}
	txt := svc.ExportText(frames)
	if !strings.Contains(txt, "[2026-01-01T12:00:00Z] event/message: hello") {
		t.Errorf("text export: want event line, got %q", txt)
	}
}

func TestPlaybackService_ExportText_EmptyFramesEmptyString(t *testing.T) {
	db := openPlaybackTestDB(t)
	svc := NewPlaybackService(db)
	if got := svc.ExportText(nil); got != "" {
		t.Errorf("empty text: want empty string, got %q", got)
	}
}
