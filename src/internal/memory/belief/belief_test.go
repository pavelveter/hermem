package belief_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/pavelveter/hermem/src/internal/memory/belief"
	"github.com/pavelveter/hermem/src/internal/store"
)

func TestService_CreateAndGet_HappyPath(t *testing.T) {
	t.Parallel()
	db := memDB(t)
	svc := belief.New(db)
	ctx := context.Background()

	b := &belief.Belief{
		Content:    "the kitchen light is on",
		Confidence: 0.85,
		SourceKind: "explicit",
		SourceID:   "user-input-1",
	}
	if err := svc.CreateBelief(ctx, b); err != nil {
		t.Fatalf("CreateBelief: %v", err)
	}
	if b.ID == 0 {
		t.Fatal("want non-zero ID post-insert")
	}
	if b.Status != belief.StatusActive {
		t.Fatalf("want default status %q, got %q", belief.StatusActive, b.Status)
	}
	if b.CreatedAt.IsZero() || b.UpdatedAt.IsZero() {
		t.Fatal("want created_at/updated_at populated")
	}

	got, err := svc.GetBelief(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetBelief: %v", err)
	}
	if got.Content != b.Content {
		t.Fatalf("Content: want %q, got %q", b.Content, got.Content)
	}
	if got.Confidence != 0.85 {
		t.Fatalf("Confidence: want 0.85, got %v", got.Confidence)
	}
	if got.SourceKind != "explicit" {
		t.Fatalf("SourceKind: want explicit, got %q", got.SourceKind)
	}
	if got.SourceID != "user-input-1" {
		t.Fatalf("SourceID: want user-input-1, got %q", got.SourceID)
	}
}

func TestService_CreateBelief_ConfidenceBoundsRejected(t *testing.T) {
	t.Parallel()
	svc := belief.New(memDB(t))
	ctx := context.Background()

	if err := svc.CreateBelief(ctx, &belief.Belief{Content: "neg", Confidence: -0.1}); err == nil {
		t.Fatal("want error for Confidence < 0")
	}
	if err := svc.CreateBelief(ctx, &belief.Belief{Content: "over", Confidence: 1.5}); err == nil {
		t.Fatal("want error for Confidence > 1")
	}
}

func TestService_NilBeliefRejected(t *testing.T) {
	t.Parallel()
	svc := belief.New(memDB(t))
	if err := svc.CreateBelief(context.Background(), nil); err == nil {
		t.Fatal("want error for nil Belief")
	}
}

func TestService_EmptyContentRejected(t *testing.T) {
	t.Parallel()
	svc := belief.New(memDB(t))
	if err := svc.CreateBelief(context.Background(), &belief.Belief{}); err == nil {
		t.Fatal("want error for empty Content")
	}
}

func TestService_DefaultConfidenceMagnitude(t *testing.T) {
	t.Parallel()
	svc := belief.New(memDB(t))
	b := &belief.Belief{Content: "default-confidence case"}
	if err := svc.CreateBelief(context.Background(), b); err != nil {
		t.Fatalf("CreateBelief: %v", err)
	}
	if b.Confidence != 1.0 {
		t.Fatalf("want default confidence 1.0, got %v", b.Confidence)
	}
}

func TestService_ListBeliefsOrdered(t *testing.T) {
	t.Parallel()
	svc := belief.New(memDB(t))
	ctx := context.Background()

	for _, content := range []string{"alpha", "beta", "gamma"} {
		if err := svc.CreateBelief(ctx, &belief.Belief{
			Content: content, Confidence: 0.9,
		}); err != nil {
			t.Fatalf("seed %q: %v", content, err)
		}
	}
	listed, err := svc.ListBeliefs(ctx)
	if err != nil {
		t.Fatalf("ListBeliefs: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("want 3 listed, got %d", len(listed))
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, w := range want {
		if listed[i].Content != w {
			t.Fatalf("row %d: want %q, got %q", i, w, listed[i].Content)
		}
	}
}

func TestService_UpdateConfidence(t *testing.T) {
	t.Parallel()
	svc := belief.New(memDB(t))
	ctx := context.Background()

	b := &belief.Belief{Content: "subject", Confidence: 0.5}
	if err := svc.CreateBelief(ctx, b); err != nil {
		t.Fatalf("CreateBelief: %v", err)
	}
	if err := svc.UpdateConfidence(ctx, b.ID, 0.95); err != nil {
		t.Fatalf("UpdateConfidence: %v", err)
	}
	got, err := svc.GetBelief(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetBelief: %v", err)
	}
	if got.Confidence != 0.95 {
		t.Fatalf("want confidence 0.95, got %v", got.Confidence)
	}
	if got.LastAccessedAt == nil {
		t.Fatal("want last_accessed_at populated")
	}
	if got.UpdatedAt.Equal(b.UpdatedAt) {
		t.Fatal("want updated_at to bump on UpdateConfidence")
	}

	if err := svc.UpdateConfidence(ctx, b.ID, 1.5); err == nil {
		t.Fatal("want error for confidence > 1.0")
	}
	if err := svc.UpdateConfidence(ctx, b.ID, -0.1); err == nil {
		t.Fatal("want error for confidence < 0.0")
	}
	if err := svc.UpdateConfidence(ctx, 9999, 0.5); !errors.Is(err, belief.ErrNotFound) {
		t.Fatalf("want ErrNotFound for missing ID, got %v", err)
	}
	if err := svc.UpdateConfidence(ctx, 0, 0.5); !errors.Is(err, belief.ErrNotFound) {
		t.Fatalf("want ErrNotFound for non-positive ID, got %v", err)
	}
}

func TestService_MarkSuperseded(t *testing.T) {
	t.Parallel()
	svc := belief.New(memDB(t))
	ctx := context.Background()

	a := &belief.Belief{Content: "old", Confidence: 0.6}
	b := &belief.Belief{Content: "new", Confidence: 0.99}
	if err := svc.CreateBelief(ctx, a); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := svc.CreateBelief(ctx, b); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	if err := svc.MarkSuperseded(ctx, a.ID, b.ID); err != nil {
		t.Fatalf("MarkSuperseded: %v", err)
	}

	got, err := svc.GetBelief(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetBelief: %v", err)
	}
	if got.Status != belief.StatusSuperseded {
		t.Fatalf("want StatusSuperseded, got %q", got.Status)
	}
	if got.SupersededBy == nil || *got.SupersededBy != b.ID {
		t.Fatalf("want SupersededBy=%d, got %v", b.ID, got.SupersededBy)
	}
	if got.ArchivedAt == nil {
		t.Fatal("want ArchivedAt populated")
	}

	if err := svc.MarkSuperseded(ctx, b.ID, b.ID); err == nil {
		t.Fatal("want error when superseded_by == id")
	}
	if err := svc.MarkSuperseded(ctx, b.ID, 9999); err == nil {
		t.Fatal("want error for missing target")
	}
	if err := svc.MarkSuperseded(ctx, 9999, b.ID); !errors.Is(err, belief.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := svc.MarkSuperseded(ctx, 0, b.ID); !errors.Is(err, belief.ErrNotFound) {
		t.Fatalf("want ErrNotFound for non-positive ID, got %v", err)
	}
	if err := svc.MarkSuperseded(ctx, a.ID, 0); !errors.Is(err, belief.ErrNotFound) {
		t.Fatalf("want ErrNotFound for non-positive byID, got %v", err)
	}
}

func TestService_GetNotFound(t *testing.T) {
	t.Parallel()
	svc := belief.New(memDB(t))
	if _, err := svc.GetBelief(context.Background(), 99999); !errors.Is(err, belief.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := svc.GetBelief(context.Background(), 0); !errors.Is(err, belief.ErrNotFound) {
		t.Fatalf("want ErrNotFound for ID 0, got %v", err)
	}
	if _, err := svc.GetBelief(context.Background(), -1); !errors.Is(err, belief.ErrNotFound) {
		t.Fatalf("want ErrNotFound for negative ID, got %v", err)
	}
}

func memDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("store.MemDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
