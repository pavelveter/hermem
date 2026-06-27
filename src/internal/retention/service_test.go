package retention_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retention"
	"github.com/pavelveter/hermem/src/internal/store"
)

// stubVI satisfies core.VectorIndex for tests that don't exercise the
// in-memory search index. Remove is the only method the production
// Service calls; tests verify the post-commit vi.Remove contract via
// snapshotRemoved. Remove always succeeds — there is no error branch
// in production that the test should reproduce (RunOnce deliberately
// drops vi.Remove's return value; it logs but does NOT report, so
// tests cannot meaningfully observe a Remove error).
type stubVI struct {
	mu      sync.Mutex
	removed []string
}

func (s *stubVI) Search(_ context.Context, _ []float32, _ int) ([]string, error) {
	return nil, nil
}
func (s *stubVI) SearchBatch(_ context.Context, _ [][]float32, _ int) ([][]string, error) {
	return nil, nil
}
func (s *stubVI) Store(_ context.Context, _ string, _ []float32) error { return nil }
func (s *stubVI) Remove(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removed = append(s.removed, ids...)
	return nil
}

func (s *stubVI) snapshotRemoved() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.removed))
	copy(out, s.removed)
	return out
}

// seedObservation inserts one observation row with a controllable
// updated_at so tests can dial in the TTL relationship deterministically.
func seedObservation(t *testing.T, db *sql.DB, id string, updatedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, updated_at, archived) VALUES (?, 'observation', ?, ?, ?, 0)`,
		id, "stale observation", []byte{}, updatedAt,
	); err != nil {
		t.Fatalf("seed observation: %v", err)
	}
}

func TestService_NewService_NotNil(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	defer db.Close()
	vi := &stubVI{}
	svc := retention.New(db, vi)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestService_RunOnce_HappyPath_ArchivesExpiredObservation(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	defer db.Close()
	vi := &stubVI{}
	svc := retention.New(db, vi)

	// Seed an observation updated_at well before the cutoff.
	seedObservation(t, db, "obs-stale-1", time.Now().Add(-2*time.Hour))
	seedObservation(t, db, "obs-fresh-1", time.Now().Add(-1*time.Minute)) // not stale

	pol := core.RetentionPolicy{
		ObservationTTL:  1 * time.Hour,
		RunInterval:     1 * time.Hour,
		DeleteBatchSize: 50,
	}

	rep, err := svc.RunOnce(t.Context(), pol)
	if err != nil {
		t.Fatalf("RunOnce: %v report=%+v", err, rep)
	}
	if rep.Error != "" {
		t.Errorf("Error field unexpectedly set: %q", rep.Error)
	}
	if rep.Swept < 1 {
		t.Errorf("Swept=%d, want >= 1", rep.Swept)
	}
	if got := vi.snapshotRemoved(); len(got) == 0 {
		t.Errorf("vi.Remove called with empty slice; want archived ids")
	}
}

func TestService_RunOnce_ZeroCandidates_ReturnsZeroSwept(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	defer db.Close()
	vi := &stubVI{}
	svc := retention.New(db, vi)

	pol := core.RetentionPolicy{
		ObservationTTL:  1 * time.Hour,
		RunInterval:     1 * time.Hour,
		DeleteBatchSize: 50,
	}

	rep, err := svc.RunOnce(t.Context(), pol)
	if err != nil {
		t.Fatalf("RunOnce on empty DB: %v", err)
	}
	if rep.Swept != 0 {
		t.Errorf("Swept=%d, want 0 (empty DB)", rep.Swept)
	}
	if rep.Error != "" {
		t.Errorf("Error=%q, want empty", rep.Error)
	}
	if rep.StartedAt.IsZero() || rep.FinishedAt.IsZero() {
		t.Errorf("timestamps not populated: started=%v finished=%v", rep.StartedAt, rep.FinishedAt)
	}
	if !rep.FinishedAt.After(rep.StartedAt) && !rep.FinishedAt.Equal(rep.StartedAt) {
		t.Errorf("FinishedAt %v must be >= StartedAt %v", rep.FinishedAt, rep.StartedAt)
	}
}

func TestService_RunOnce_TTL_AlreadyExpired_ArchivesImmediately(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	defer db.Close()
	vi := &stubVI{}
	svc := retention.New(db, vi)

	// updated_at is 1 ns ago; ObservationTTL=0 means cutoff = now, so
	// everything older than now is archived. The row qualifies by 1 ns.
	seedObservation(t, db, "obs-edge", time.Now().Add(-1*time.Nanosecond))

	pol := core.RetentionPolicy{
		ObservationTTL:  0,
		RunInterval:     1 * time.Hour,
		DeleteBatchSize: 50,
	}

	rep, err := svc.RunOnce(t.Context(), pol)
	if err != nil {
		t.Fatalf("RunOnce on edge-TTL row: %v", err)
	}
	if rep.Swept < 1 {
		t.Errorf("Swept=%d, want >= 1 (TTL=0 archives immediately)", rep.Swept)
	}
}

func TestService_RunOnce_CancelledContext_ReturnsError(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	defer db.Close()
	vi := &stubVI{}
	svc := retention.New(db, vi)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel BEFORE call

	pol := core.RetentionPolicy{
		ObservationTTL:  1 * time.Hour,
		RunInterval:     1 * time.Hour,
		DeleteBatchSize: 50,
	}

	rep, err := svc.RunOnce(ctx, pol)
	if err == nil {
		t.Fatal("expected error from cancelled-context RunOnce, got nil")
	}
	if rep.Swept != 0 {
		t.Errorf("Swept=%d on cancelled ctx, want 0", rep.Swept)
	}
}

func TestService_Run_RespectsContextCancel(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	defer db.Close()
	vi := &stubVI{}
	svc := retention.New(db, vi)

	ctx, cancel := context.WithCancel(t.Context())
	pol := core.RetentionPolicy{
		ObservationTTL:  24 * time.Hour,
		RunInterval:     10 * time.Millisecond,
		DeleteBatchSize: 50,
	}

	done := make(chan struct{})
	go func() {
		svc.Run(ctx, pol)
		close(done)
	}()

	// Let the loop run a couple of ticks, then cancel.
	time.Sleep(25 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// ok — Run returned after ctx cancel
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
}
