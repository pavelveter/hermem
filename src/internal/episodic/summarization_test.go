package episodic

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pavelveter/hermem/src/internal/core"
)

// openSummarizationTestDB returns an in-memory SQLite with the
// schema summarisation needs: episodes + entities + events +
// episode_memories (so we can build a realistic episode to
// summarise).
func openSummarizationTestDB(t *testing.T) *sql.DB {
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
			content TEXT NOT NULL DEFAULT ''
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
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec schema: %v\n%s", err, s)
		}
	}
	return db
}

// stubExtractor records every dialog it sees and returns a
// canned ExtractionResult so tests can assert what was passed
// to the LLM and what the resulting summary looks like.
type stubExtractor struct {
	dialogs []string
	result  *core.ExtractionResult
	err     error
}

func (s *stubExtractor) ExtractEntities(_ context.Context, dialog string) (*core.ExtractionResult, error) {
	s.dialogs = append(s.dialogs, dialog)
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func TestSummarizer_RequiresEpisodeID(t *testing.T) {
	db := openSummarizationTestDB(t)
	ext := &stubExtractor{}
	s := NewSummarizer(db, ext)
	_, err := s.SummarizeEpisode(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestSummarizer_EpisodeNotFound(t *testing.T) {
	db := openSummarizationTestDB(t)
	ext := &stubExtractor{}
	s := NewSummarizer(db, ext)
	_, err := s.SummarizeEpisode(t.Context(), "missing")
	if err == nil {
		t.Fatal("want error for missing episode, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSummarizer_EmptyEpisodePersistsEmptySummary(t *testing.T) {
	db := openSummarizationTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-empty')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	ext := &stubExtractor{}
	s := NewSummarizer(db, ext)
	summary, err := s.SummarizeEpisode(t.Context(), "ep-empty")
	if err != nil {
		t.Fatalf("SummarizeEpisode: %v", err)
	}
	if summary != "" {
		t.Fatalf("empty episode: want empty summary, got %q", summary)
	}
	// Extractor was NOT called — empty dialog short-circuits.
	if len(ext.dialogs) != 0 {
		t.Fatalf("extractor should not be called for empty dialog, got %d", len(ext.dialogs))
	}
	// And the summary column was persisted (even if empty).
	ep, err := New(db).GetEpisode(t.Context(), "ep-empty")
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if ep.Summary != "" {
		t.Fatalf("persisted summary: want empty, got %q", ep.Summary)
	}
}

func TestSummarizer_BuildsDialogFromEventsAndMemories(t *testing.T) {
	db := openSummarizationTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'memory content')`); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	evSvc := NewEventService(db)
	for _, e := range []Event{
		{ID: "e1", EpisodeID: "ep-1", Type: EventMessage, Content: "hello"},
		{ID: "e2", EpisodeID: "ep-1", Type: EventMessage, Content: "world"},
	} {
		if err := evSvc.CreateEvent(t.Context(), e); err != nil {
			t.Fatalf("CreateEvent: %v", err)
		}
	}
	linkSvc := NewLinkService(db)
	if err := linkSvc.LinkMemory(t.Context(), "ep-1", "m1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	ext := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{{Category: "world", Content: "summary fact"}},
	}}
	s := NewSummarizer(db, ext)
	summary, err := s.SummarizeEpisode(t.Context(), "ep-1")
	if err != nil {
		t.Fatalf("SummarizeEpisode: %v", err)
	}
	if len(ext.dialogs) != 1 {
		t.Fatalf("extractor calls: want 1, got %d", len(ext.dialogs))
	}
	dialog := ext.dialogs[0]
	if !strings.Contains(dialog, "[message] hello") {
		t.Errorf("dialog missing first event; got: %q", dialog)
	}
	if !strings.Contains(dialog, "[message] world") {
		t.Errorf("dialog missing second event; got: %q", dialog)
	}
	if !strings.Contains(dialog, "[memory:extracted] memory content") {
		t.Errorf("dialog missing memory; got: %q", dialog)
	}
	if !strings.Contains(summary, "[world] summary fact") {
		t.Errorf("summary missing extracted entity; got: %q", summary)
	}
}

func TestSummarizer_PersistsSummaryToEpisode(t *testing.T) {
	db := openSummarizationTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	// Seed one event so the dialog is non-empty and the extractor
	// actually runs (empty episodes short-circuit before extraction).
	if err := NewEventService(db).CreateEvent(t.Context(), Event{
		ID: "e1", EpisodeID: "ep-1", Type: EventMessage, Content: "seed",
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	ext := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{Category: "world", Content: "alpha"},
			{Category: "opinion", Content: "beta"},
		},
	}}
	s := NewSummarizer(db, ext)
	_, err := s.SummarizeEpisode(t.Context(), "ep-1")
	if err != nil {
		t.Fatalf("SummarizeEpisode: %v", err)
	}
	ep, _ := New(db).GetEpisode(t.Context(), "ep-1")
	if !strings.Contains(ep.Summary, "[world] alpha") {
		t.Errorf("persisted summary missing first entity: %q", ep.Summary)
	}
	if !strings.Contains(ep.Summary, "[opinion] beta") {
		t.Errorf("persisted summary missing second entity: %q", ep.Summary)
	}
}

func TestSummarizer_PropagatesExtractorError(t *testing.T) {
	db := openSummarizationTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'mem')`); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	linkSvc := NewLinkService(db)
	if err := linkSvc.LinkMemory(t.Context(), "ep-1", "m1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	sentinel := errors.New("llm-down")
	ext := &stubExtractor{err: sentinel}
	s := NewSummarizer(db, ext)
	_, err := s.SummarizeEpisode(t.Context(), "ep-1")
	if err == nil {
		t.Fatal("want extractor error to propagate, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error in chain, got %v", err)
	}
	// And the summary must NOT have been persisted on error.
	ep, _ := New(db).GetEpisode(t.Context(), "ep-1")
	if ep.Summary != "" {
		t.Fatalf("on error: want empty summary, got %q", ep.Summary)
	}
}

func TestSummarizer_EmptyExtractionRendersPlaceholder(t *testing.T) {
	db := openSummarizationTestDB(t)
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES ('ep-1')`); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	// Seed one event so the dialog is non-empty and the extractor
	// actually runs (empty episodes short-circuit before extraction).
	if err := NewEventService(db).CreateEvent(t.Context(), Event{
		ID: "e1", EpisodeID: "ep-1", Type: EventMessage, Content: "seed",
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	ext := &stubExtractor{result: &core.ExtractionResult{Entities: nil}}
	s := NewSummarizer(db, ext)
	summary, err := s.SummarizeEpisode(t.Context(), "ep-1")
	if err != nil {
		t.Fatalf("SummarizeEpisode: %v", err)
	}
	if summary != "(no entities extracted)" {
		t.Fatalf("empty extraction: want placeholder, got %q", summary)
	}
}

func TestBuildSummarizationDialog_OrdersEventsThenMemories(t *testing.T) {
	events := []Event{
		{Type: EventMessage, Content: "e1"},
		{Type: EventAction, Content: "e2"},
	}
	memories := []MemoryRef{
		{Role: "extracted", Content: "m1"},
	}
	got := buildSummarizationDialog(events, memories)
	wantOrder := []string{"[message] e1", "[action] e2", "[memory:extracted] m1"}
	// Check that each line appears in order.
	lastIdx := -1
	for _, want := range wantOrder {
		idx := strings.Index(got, want)
		if idx == -1 {
			t.Fatalf("missing %q in dialog:\n%s", want, got)
		}
		if idx <= lastIdx {
			t.Fatalf("line %q appears out of order (idx=%d, lastIdx=%d):\n%s", want, idx, lastIdx, got)
		}
		lastIdx = idx
	}
}

func TestBuildSummarizationDialog_EmptyInputsReturnsEmpty(t *testing.T) {
	if got := buildSummarizationDialog(nil, nil); got != "" {
		t.Fatalf("empty inputs: want empty string, got %q", got)
	}
}

func TestFormatSummaryFromExtraction_EmptyResultPlaceholder(t *testing.T) {
	got := formatSummaryFromExtraction(nil)
	if got != "(no entities extracted)" {
		t.Fatalf("nil result: want placeholder, got %q", got)
	}
	got = formatSummaryFromExtraction(&core.ExtractionResult{})
	if got != "(no entities extracted)" {
		t.Fatalf("empty entities: want placeholder, got %q", got)
	}
}

func TestFormatSummaryFromExtraction_Bullets(t *testing.T) {
	got := formatSummaryFromExtraction(&core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{Category: "world", Content: "alpha"},
			{Category: "opinion", Content: "beta"},
		},
	})
	want := "- [world] alpha\n- [opinion] beta"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
