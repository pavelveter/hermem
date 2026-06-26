package episodic

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openRetrievalTestDB returns an in-memory SQLite with the
// retrieval schema: episodes + sessions + episode_memories (for
// the HasLinkedMemories filter).
func openRetrievalTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
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
			content TEXT NOT NULL DEFAULT ''
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

// stubEmbedder returns a fixed vector for any non-empty input.
// Tests that need variable embeddings build their own.
type stubEmbedder struct {
	vec []float32
}

func (s *stubEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	if content == "" {
		return nil, nil
	}
	out := make([]float32, len(s.vec))
	copy(out, s.vec)
	return out, nil
}

// seedEpisodeWithMeta inserts an episode with explicit metadata
// (must be valid JSON).
func seedEpisodeWithMeta(t *testing.T, db *sql.DB, id, sessionID, summary string, startedAt time.Time, meta string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO episodes (id, session_id, summary, started_at, metadata) VALUES (?, ?, ?, ?, ?)`,
		id, sql.NullString{String: sessionID, Valid: sessionID != ""}, summary, startedAt, meta,
	); err != nil {
		t.Fatalf("seed episode %s: %v", id, err)
	}
}

func TestRetrievalService_SearchEpisodes_NoFiltersReturnsAllOrderedDesc(t *testing.T) {
	db := openRetrievalTestDB(t)
	epSvc := New(db)
	for i, id := range []string{"ep-1", "ep-2", "ep-3"} {
		if err := epSvc.CreateEpisode(context.Background(), Episode{
			ID:        id,
			StartedAt: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("CreateEpisode %s: %v", id, err)
		}
	}
	svc := NewRetrievalService(db, nil)
	got, err := svc.SearchEpisodes(context.Background(), "", EpisodeFilter{})
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 episodes, got %d", len(got))
	}
	wantIDs := []string{"ep-3", "ep-2", "ep-1"} // DESC
	for i, e := range got {
		if e.ID != wantIDs[i] {
			t.Errorf("position %d: want %s, got %s", i, wantIDs[i], e.ID)
		}
	}
}

func TestRetrievalService_SearchEpisodes_FilterBySessionID(t *testing.T) {
	db := openRetrievalTestDB(t)
	if _, err := db.Exec(`INSERT INTO sessions (id) VALUES ('sess-1')`); err != nil {
		t.Fatalf("seed session 1: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id) VALUES ('sess-2')`); err != nil {
		t.Fatalf("seed session 2: %v", err)
	}
	epSvc := New(db)
	for _, e := range []Episode{
		{ID: "ep-1", SessionID: "sess-1", StartedAt: time.Now()},
		{ID: "ep-2", SessionID: "sess-1", StartedAt: time.Now()},
		{ID: "ep-3", SessionID: "sess-2", StartedAt: time.Now()},
	} {
		if err := epSvc.CreateEpisode(context.Background(), e); err != nil {
			t.Fatalf("CreateEpisode %s: %v", e.ID, err)
		}
	}
	svc := NewRetrievalService(db, nil)
	got, err := svc.SearchEpisodes(context.Background(), "", EpisodeFilter{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("sess-1: want 2 episodes, got %d", len(got))
	}
	for _, e := range got {
		if e.SessionID != "sess-1" {
			t.Errorf("episode %s from session %s; want sess-1", e.ID, e.SessionID)
		}
	}
}

func TestRetrievalService_SearchEpisodes_FilterByTimeRange(t *testing.T) {
	db := openRetrievalTestDB(t)
	epSvc := New(db)
	for i, id := range []string{"jan", "feb", "mar"} {
		if err := epSvc.CreateEpisode(context.Background(), Episode{
			ID:        id,
			StartedAt: time.Date(2026, time.Month(i+1), 1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("CreateEpisode %s: %v", id, err)
		}
	}
	svc := NewRetrievalService(db, nil)
	got, err := svc.SearchEpisodes(context.Background(), "", EpisodeFilter{
		TimeFrom: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		TimeTo:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want feb+mar, got %d", len(got))
	}
}

func TestRetrievalService_SearchEpisodes_FilterHasSummary(t *testing.T) {
	db := openRetrievalTestDB(t)
	epSvc := New(db)
	if err := epSvc.CreateEpisode(context.Background(), Episode{ID: "ep-summary", StartedAt: time.Now()}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	if err := epSvc.UpdateSummary(context.Background(), "ep-summary", "has summary"); err != nil {
		t.Fatalf("UpdateSummary: %v", err)
	}
	if err := epSvc.CreateEpisode(context.Background(), Episode{ID: "ep-empty", StartedAt: time.Now()}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	svc := NewRetrievalService(db, nil)
	got, err := svc.SearchEpisodes(context.Background(), "", EpisodeFilter{HasSummary: true})
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 episode with summary, got %d", len(got))
	}
	if got[0].ID != "ep-summary" {
		t.Errorf("want ep-summary, got %s", got[0].ID)
	}
}

func TestRetrievalService_SearchEpisodes_FilterHasLinkedMemories(t *testing.T) {
	db := openRetrievalTestDB(t)
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('m1', 'world', 'mem')`); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	epSvc := New(db)
	if err := epSvc.CreateEpisode(context.Background(), Episode{ID: "ep-linked", StartedAt: time.Now()}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	if err := epSvc.CreateEpisode(context.Background(), Episode{ID: "ep-unlinked", StartedAt: time.Now()}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	linkSvc := NewLinkService(db)
	if err := linkSvc.LinkMemory(context.Background(), "ep-linked", "m1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	svc := NewRetrievalService(db, nil)
	got, err := svc.SearchEpisodes(context.Background(), "", EpisodeFilter{HasLinkedMemories: true})
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 episode with linked memories, got %d", len(got))
	}
	if got[0].ID != "ep-linked" {
		t.Errorf("want ep-linked, got %s", got[0].ID)
	}
}

func TestRetrievalService_SearchEpisodes_Limit(t *testing.T) {
	db := openRetrievalTestDB(t)
	epSvc := New(db)
	for i := 0; i < 5; i++ {
		if err := epSvc.CreateEpisode(context.Background(), Episode{
			ID:        []string{"a", "b", "c", "d", "e"}[i],
			StartedAt: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("CreateEpisode: %v", err)
		}
	}
	svc := NewRetrievalService(db, nil)
	got, err := svc.SearchEpisodes(context.Background(), "", EpisodeFilter{Limit: 2})
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("limit=2: want 2, got %d", len(got))
	}
}

func TestRetrievalService_SearchEpisodes_SemanticRerank(t *testing.T) {
	db := openRetrievalTestDB(t)
	// Two episodes with stored embeddings; one without. Embedder
	// returns a vector that matches ep-1's stored embedding.
	ep1Meta := `{"embedding": [1.0, 0.0, 0.0]}`
	ep2Meta := `{"embedding": [0.0, 1.0, 0.0]}`
	ep3Meta := `{}` // no embedding → cosine=0 → cluster at bottom
	seedEpisodeWithMeta(t, db, "ep-1", "", "summary 1", time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), ep1Meta)
	seedEpisodeWithMeta(t, db, "ep-2", "", "summary 2", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), ep2Meta)
	seedEpisodeWithMeta(t, db, "ep-3", "", "summary 3", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ep3Meta)

	emb := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	svc := NewRetrievalService(db, emb)
	got, err := svc.SearchEpisodes(context.Background(), "anything", EpisodeFilter{})
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 episodes, got %d", len(got))
	}
	// ep-1 has cosine 1.0 (match), ep-2 has 0.0, ep-3 has no embedding (0.0).
	// ep-1 must come first; ep-2 and ep-3 tiebreak by original SQL order (started_at DESC: ep-2, ep-3).
	if got[0].ID != "ep-1" {
		t.Errorf("top result: want ep-1, got %s", got[0].ID)
	}
}

func TestRetrievalService_SearchEpisodes_NoEmbedderIsPureSQL(t *testing.T) {
	db := openRetrievalTestDB(t)
	epSvc := New(db)
	if err := epSvc.CreateEpisode(context.Background(), Episode{ID: "ep-1", StartedAt: time.Now()}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	svc := NewRetrievalService(db, nil) // nil embedder
	// Passing a non-empty query with nil embedder should NOT error
	// — it just falls back to pure SQL ordering.
	got, err := svc.SearchEpisodes(context.Background(), "anything", EpisodeFilter{})
	if err != nil {
		t.Fatalf("SearchEpisodes with nil embedder: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 episode, got %d", len(got))
	}
}

func TestRetrievalService_EmptyResultReturnsEmptySlice(t *testing.T) {
	db := openRetrievalTestDB(t)
	svc := NewRetrievalService(db, nil)
	got, err := svc.SearchEpisodes(context.Background(), "", EpisodeFilter{})
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 episodes, got %d", len(got))
	}
}

func TestCosineAgainstMetadata_HandlesFloat64Slice(t *testing.T) {
	// Some callers may store []float64 directly in metadata rather
	// than the JSON-roundtrip []any shape — verify the fallback path.
	scores := make(map[string]float64)
	for _, meta := range []map[string]any{
		{"embedding": []float64{1.0, 0.0, 0.0}},
		{"embedding": []any{float64(1.0), float64(0.0), float64(0.0)}},
		{"embedding": []float32{1.0, 0.0, 0.0}}, // []float32 falls through to 0 (unsupported in fallback)
		{}, // missing key
	} {
		got := cosineAgainstMetadata([]float32{1.0, 0.0, 0.0}, meta)
		// First two should be 1.0; last two should be 0.
		switch len(scores) {
		case 0:
			if got != 1.0 {
				t.Errorf("[]float64 embedding: want 1.0, got %v", got)
			}
		case 1:
			if got != 1.0 {
				t.Errorf("[]any embedding: want 1.0, got %v", got)
			}
		case 2:
			// []float32 is not handled by the typed assertion;
			// the fallback returns 0 (consistent with documented
			// behavior — episodes without a JSON-roundtrip
			// embedding are pushed to the bottom).
			if got != 0 {
				t.Errorf("[]float32 embedding: want 0, got %v", got)
			}
		case 3:
			if got != 0 {
				t.Errorf("missing key: want 0, got %v", got)
			}
		}
		scores[fmt.Sprintf("case-%d", len(scores))] = got
	}
	_ = scores
}
