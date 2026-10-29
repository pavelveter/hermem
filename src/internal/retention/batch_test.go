package retention_test

import (
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/retention"
	"github.com/pavelveter/hermem/src/internal/store"
)

// TestService_RunOnce_ArchivesWholeBatch covers the batched archive UPDATE:
// every selected row must be archived, not just the first, and vi must be told
// to drop exactly the archived IDs after the commit.
func TestService_RunOnce_ArchivesWholeBatch(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	defer db.Close()
	vi := &stubVI{}
	svc := retention.New(db, vi)

	ids := []string{"obs-a", "obs-b", "obs-c", "obs-d"}
	for _, id := range ids {
		seedObservation(t, db, id, time.Now().Add(-3*time.Hour))
	}
	seedObservation(t, db, "obs-fresh", time.Now().Add(-1*time.Minute))

	rep, err := svc.RunOnce(t.Context(), retention.Policy{
		ObservationTTL:  time.Hour,
		RunInterval:     time.Hour,
		DeleteBatchSize: 50,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v report=%+v", err, rep)
	}
	if rep.Swept != len(ids) {
		t.Fatalf("Swept=%d, want %d", rep.Swept, len(ids))
	}

	for _, id := range ids {
		var archived int
		if err := db.QueryRow(`SELECT archived FROM entities WHERE id = ?`, id).Scan(&archived); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if archived != 1 {
			t.Errorf("%s: archived=%d, want 1", id, archived)
		}
	}
	var freshArchived int
	if err := db.QueryRow(`SELECT archived FROM entities WHERE id = 'obs-fresh'`).Scan(&freshArchived); err != nil {
		t.Fatalf("read fresh: %v", err)
	}
	if freshArchived != 0 {
		t.Errorf("fresh observation archived; want untouched")
	}

	removed := vi.snapshotRemoved()
	if len(removed) != len(ids) {
		t.Fatalf("vi.Remove got %d ids, want %d: %v", len(removed), len(ids), removed)
	}
}
