package episodic

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pavelveter/hermem/src/internal/core"
)

// randomSuffix returns a short hex string for unique shared-cache
// names per test. rand.Read is safe for testing (non-crypto use).
func randomSuffix() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// openIntegrationDB returns a fresh in-memory SQLite with the
// full episodic schema applied (sessions, conversations,
// episodes, entities, events, episode_memories, episode_tasks).
// Each call gets its own *sql.DB so tests are safe to run with
// t.Parallel — no shared state across tests.
//
// Uses SQLite's `file::memory:?cache=shared` URI with a
// per-call unique name so connections in Go's database/sql pool
// share the SAME in-memory database within a test (so schema +
// data created on one connection is visible on another — needed
// for the parallel subtest that fans out to multiple goroutines),
// while DIFFERENT tests get DIFFERENT databases (so seed data
// from one test doesn't bleed into another).
func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	// Per-test unique shared-cache name. The `name=...` parameter
	// scopes the shared cache; without it every test would share
	// one database and seed data would collide (UNIQUE constraint
	// failures on sessions.id).
	dbName := "episodic_test_" + t.Name() + "_" + randomSuffix()
	dsn := "file:" + dbName + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Limit to a single connection so the in-memory database is
	// accessed serially within a test — SQLite shared in-memory
	// has stricter locking semantics across connections.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at DATETIME,
			metadata TEXT DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			summary TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS episodes (
			id TEXT PRIMARY KEY,
			session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
			conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
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

// TestIntegration_FullPipeline runs the entire episodic subsystem
// in sequence and verifies each stage's output feeds the next:
//
//   CreateEpisode → CreateEvent ×N → LinkMemory ×N → LinkTask ×N
//   → ReconstructTimeline → SummarizeEpisode → Playback
//
// One test exercises every Service in the package and asserts
// the final playback reflects the events/memories/tasks that
// were created earlier.
func TestIntegration_FullPipeline(t *testing.T) {
	t.Parallel()
	db := openIntegrationDB(t)
	ctx := context.Background()

	// 1. Create a session + episode.
	if _, err := db.Exec(`INSERT INTO sessions (id) VALUES ('sess-1')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	epSvc := New(db)
	if err := epSvc.CreateEpisode(ctx, Episode{
		ID:        "ep-int",
		SessionID: "sess-1",
		Title:     "integration test episode",
	}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}

	// 2. Add 3 events with explicit timestamps (chronological order).
	evSvc := NewEventService(db)
	for i, e := range []Event{
		{ID: "ev-1", EpisodeID: "ep-int", Type: EventMessage, Content: "hello",
			Timestamp: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)},
		{ID: "ev-2", EpisodeID: "ep-int", Type: EventAction, Content: "clicked",
			Timestamp: time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC)},
		{ID: "ev-3", EpisodeID: "ep-int", Type: EventObservation, Content: "saw",
			Timestamp: time.Date(2026, 1, 1, 10, 10, 0, 0, time.UTC)},
	} {
		if err := evSvc.CreateEvent(ctx, e); err != nil {
			t.Fatalf("CreateEvent %d: %v", i, err)
		}
	}

	// 3. Link 2 memory entities.
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'memory one')`); err != nil {
		t.Fatalf("seed m1: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m2', 'opinion', 'memory two')`); err != nil {
		t.Fatalf("seed m2: %v", err)
	}
	linkSvc := NewLinkService(db)
	if err := linkSvc.LinkMemory(ctx, "ep-int", "m1", "extracted"); err != nil {
		t.Fatalf("LinkMemory m1: %v", err)
	}
	if err := linkSvc.LinkMemory(ctx, "ep-int", "m2", "referenced"); err != nil {
		t.Fatalf("LinkMemory m2: %v", err)
	}

	// 4. Link 1 task entity.
	if _, err := db.Exec(`INSERT INTO entities (id, category, content, status) VALUES ('t1', 'task', 'do thing', 'running')`); err != nil {
		t.Fatalf("seed t1: %v", err)
	}
	taskSvc := NewTaskLinkService(db)
	if err := taskSvc.LinkTask(ctx, "ep-int", "t1"); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	// 5. Reconstruct timeline — should produce 6 frames
	// (3 events + 2 memories + 1 task), ordered chronologically.
	ts := NewTimelineService(db)
	entries, err := ts.ReconstructTimeline(ctx, "ep-int")
	if err != nil {
		t.Fatalf("ReconstructTimeline: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("timeline: want 6 entries, got %d", len(entries))
	}

	// 6. Summarize via stub LLM.
	ext := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{Category: "world", Content: "integration test fact"},
		},
	}}
	sum := NewSummarizer(db, ext)
	summary, err := sum.SummarizeEpisode(ctx, "ep-int")
	if err != nil {
		t.Fatalf("SummarizeEpisode: %v", err)
	}
	if !strings.Contains(summary, "[world] integration test fact") {
		t.Fatalf("summary: want extracted entity, got %q", summary)
	}
	// Extractor must have been called exactly once with a dialog
	// containing all 3 events and both memory roles.
	if len(ext.dialogs) != 1 {
		t.Fatalf("extractor calls: want 1, got %d", len(ext.dialogs))
	}
	dialog := ext.dialogs[0]
	for _, want := range []string{"hello", "clicked", "saw", "memory one", "memory two"} {
		if !strings.Contains(dialog, want) {
			t.Errorf("dialog missing %q", want)
		}
	}

	// 7. Verify persisted summary.
	got, err := epSvc.GetEpisode(ctx, "ep-int")
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if !strings.Contains(got.Summary, "[world] integration test fact") {
		t.Fatalf("persisted summary: want extracted entity, got %q", got.Summary)
	}

	// 8. Playback — should produce the same 6 frames in the same order.
	pbSvc := NewPlaybackService(db)
	frames, err := pbSvc.Playback(ctx, "ep-int")
	if err != nil {
		t.Fatalf("Playback: %v", err)
	}
	if len(frames) != 6 {
		t.Fatalf("playback: want 6 frames, got %d", len(frames))
	}
	// Actor mapping per kind (event→Type, memory→role, task→status).
	for _, f := range frames {
		switch f.Type {
		case "event":
			if f.Actor == "" {
				t.Errorf("event frame %s: want non-empty Actor (Type)", f.Source)
			}
		case "memory":
			if f.Actor == "" {
				t.Errorf("memory frame %s: want non-empty Actor (role)", f.Source)
			}
		case "task":
			if f.Actor == "" {
				t.Errorf("task frame %s: want non-empty Actor (status)", f.Source)
			}
		default:
			t.Errorf("frame %s: unknown Type %q", f.Source, f.Type)
		}
	}

	// 9. ExportMarkdown + ExportJSON + ExportText on the playback.
	md := pbSvc.ExportMarkdown(frames)
	if !strings.HasPrefix(md, "# Episode Timeline") {
		t.Errorf("markdown missing header: %q", md)
	}
	if !strings.Contains(md, "hello") || !strings.Contains(md, "memory one") {
		t.Errorf("markdown missing content: %q", md)
	}
	jsonBytes, err := pbSvc.ExportJSON(frames)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	if len(jsonBytes) == 0 {
		t.Fatalf("ExportJSON: empty output")
	}
	txt := pbSvc.ExportText(frames)
	if !strings.Contains(txt, "hello") {
		t.Errorf("text export missing content: %q", txt)
	}
}

// TestIntegration_EmptyEpisodeEdgeCase verifies that the full
// pipeline handles a valid-but-empty episode cleanly — every
// stage returns empty (not error), and the final playback is
// empty + exportable.
func TestIntegration_EmptyEpisodeEdgeCase(t *testing.T) {
	t.Parallel()
	db := openIntegrationDB(t)
	ctx := context.Background()

	if err := New(db).CreateEpisode(ctx, Episode{ID: "ep-empty", Title: "nothing happened"}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}

	entries, err := NewTimelineService(db).ReconstructTimeline(ctx, "ep-empty")
	if err != nil {
		t.Fatalf("ReconstructTimeline on empty: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty timeline: want 0 entries, got %d", len(entries))
	}

	frames, err := NewPlaybackService(db).Playback(ctx, "ep-empty")
	if err != nil {
		t.Fatalf("Playback on empty: %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("empty playback: want 0 frames, got %d", len(frames))
	}

	// Empty Markdown should still have the header.
	md := NewPlaybackService(db).ExportMarkdown(frames)
	if !strings.HasPrefix(md, "# Episode Timeline") {
		t.Errorf("empty markdown: want header, got %q", md)
	}
}

// TestIntegration_MissingLLMEdgeCase — SummarizeEpisode with a
// failing extractor must NOT persist a summary, and the error
// must propagate.
func TestIntegration_MissingLLMEdgeCase(t *testing.T) {
	t.Parallel()
	db := openIntegrationDB(t)
	ctx := context.Background()

	if err := New(db).CreateEpisode(ctx, Episode{ID: "ep-no-llm"}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	// Seed an event so the dialog is non-empty (otherwise
	// SummarizeEpisode short-circuits without calling the extractor).
	if err := NewEventService(db).CreateEvent(ctx, Event{
		ID: "e1", EpisodeID: "ep-no-llm", Type: EventMessage, Content: "hello",
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	sentinel := &stubExtractor{err: errLLMDown}
	s := NewSummarizer(db, sentinel)
	if _, err := s.SummarizeEpisode(ctx, "ep-no-llm"); err == nil {
		t.Fatal("want LLM error to propagate, got nil")
	}
	// And the summary must NOT have been persisted.
	got, _ := New(db).GetEpisode(ctx, "ep-no-llm")
	if got.Summary != "" {
		t.Fatalf("on LLM error: want empty summary, got %q", got.Summary)
	}
}

var errLLMDown = stringError("llm-down")

type stringError string

func (e stringError) Error() string { return string(e) }

// TestIntegration_ParallelSubtests — run the full pipeline under
// t.Parallel so the -race detector catches any shared-state bugs.
// Each subtest opens its own in-memory SQLite (see openIntegrationDB)
// so there is no shared mutable state between subtests.
//
// Test body does not assert the pipeline result — it only checks
// that running the pipeline concurrently under -race does not
// produce data races. The unit tests in each *_test.go file
// cover correctness; this test covers concurrency.
func TestIntegration_ParallelSubtests(t *testing.T) {
	t.Parallel()
	db := openIntegrationDB(t)
	ctx := context.Background()

	// Pre-seed minimal data so every parallel subtest has
	// something to operate on.
	if _, err := db.Exec(`INSERT INTO sessions (id) VALUES ('sess-par')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := New(db).CreateEpisode(ctx, Episode{ID: "ep-par", SessionID: "sess-par"}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}

	const subtests = 8
	var wg sync.WaitGroup
	wg.Add(subtests)
	for i := 0; i < subtests; i++ {
		go func(i int) {
			defer wg.Done()
			// Each goroutine creates events + links + plays back.
			// Operations on distinct entity ids are race-free
			// under SQLite's per-connection locking for in-memory
			// databases; the -race flag on the test binary detects
			// any Go-level data races in the Service / Summarizer
			// / Playback code itself.
			evSvc := NewEventService(db)
			eventID := "ev-par-" + string(rune('a'+i))
			if err := evSvc.CreateEvent(ctx, Event{
				ID: eventID, EpisodeID: "ep-par",
				Type: EventMessage, Content: eventID,
			}); err != nil {
				t.Errorf("CreateEvent %d: %v", i, err)
				return
			}
			linkSvc := NewLinkService(db)
			memID := "m-par-" + string(rune('a'+i))
			if _, err := db.Exec(
				`INSERT INTO entities (id, category, content) VALUES (?, 'world', ?)`,
				memID, memID); err != nil {
				t.Errorf("seed mem %d: %v", i, err)
				return
			}
			if err := linkSvc.LinkMemory(ctx, "ep-par", memID, "extracted"); err != nil {
				t.Errorf("LinkMemory %d: %v", i, err)
				return
			}
			pbSvc := NewPlaybackService(db)
			if _, err := pbSvc.Playback(ctx, "ep-par"); err != nil {
				t.Errorf("Playback %d: %v", i, err)
				return
			}
		}(i)
	}
	wg.Wait()

	// After all subtests, the episode should have `subtests`
	// events and `subtests` memory links.
	events, err := NewEventService(db).ListEventsByEpisode(ctx, "ep-par")
	if err != nil {
		t.Fatalf("ListEventsByEpisode: %v", err)
	}
	if len(events) != subtests {
		t.Errorf("parallel: want %d events, got %d", subtests, len(events))
	}
	memories, err := NewLinkService(db).ListMemoriesForEpisode(ctx, "ep-par")
	if err != nil {
		t.Fatalf("ListMemoriesForEpisode: %v", err)
	}
	if len(memories) != subtests {
		t.Errorf("parallel: want %d memories, got %d", subtests, len(memories))
	}
}

// TestIntegration_SearchWithFilters runs SearchEpisodes against
// a populated dataset and verifies each filter narrows the
// result set correctly.
func TestIntegration_SearchWithFilters(t *testing.T) {
	t.Parallel()
	db := openIntegrationDB(t)
	ctx := context.Background()

	if _, err := db.Exec(`INSERT INTO sessions (id) VALUES ('sess-1')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	epSvc := New(db)
	// Three episodes: one with summary, one linked to a memory,
	// one with both. Different sessions to test session filter.
	for i, e := range []Episode{
		{ID: "ep-a", SessionID: "sess-1", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Title: "a"},
		{ID: "ep-b", SessionID: "sess-1", StartedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Title: "b"},
	} {
		if err := epSvc.CreateEpisode(ctx, e); err != nil {
			t.Fatalf("CreateEpisode %d: %v", i, err)
		}
	}
	if err := epSvc.UpdateSummary(ctx, "ep-a", "has summary"); err != nil {
		t.Fatalf("UpdateSummary: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'mem')`); err != nil {
		t.Fatalf("seed m1: %v", err)
	}
	if err := NewLinkService(db).LinkMemory(ctx, "ep-b", "m1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}

	svc := NewRetrievalService(db, nil)
	// HasSummary filter → ep-a only.
	got, err := svc.SearchEpisodes(ctx, "", EpisodeFilter{HasSummary: true})
	if err != nil {
		t.Fatalf("SearchEpisodes HasSummary: %v", err)
	}
	if len(got) != 1 || got[0].ID != "ep-a" {
		t.Fatalf("HasSummary: want [ep-a], got %v", got)
	}
	// HasLinkedMemories filter → ep-b only.
	got, err = svc.SearchEpisodes(ctx, "", EpisodeFilter{HasLinkedMemories: true})
	if err != nil {
		t.Fatalf("SearchEpisodes HasLinkedMemories: %v", err)
	}
	if len(got) != 1 || got[0].ID != "ep-b" {
		t.Fatalf("HasLinkedMemories: want [ep-b], got %v", got)
	}
	// Time range filter → ep-b only (Feb).
	got, err = svc.SearchEpisodes(ctx, "", EpisodeFilter{
		TimeFrom: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		TimeTo:   time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SearchEpisodes TimeRange: %v", err)
	}
	if len(got) != 1 || got[0].ID != "ep-b" {
		t.Fatalf("TimeRange: want [ep-b], got %v", got)
	}
}
