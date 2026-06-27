package reembed_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/reembed"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// stubEmbedder is a deterministic 3-dim embedder matching the
// integration_test.go + edge/service_test.go stubEmbedder shape.
type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func (stubEmbedder) Ping(_ context.Context) error {
	return nil
}

// newReembedFixture wires a reembed.Service against an in-memory
// SQLite + in-memory vector index + 3-dim stub embedder.
func newReembedFixture(t *testing.T) (*reembed.Service, *sql.DB) {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	vi := vector.NewInMemoryVectorIndex(db)
	svc := reembed.New(db, vi, stubEmbedder{})
	return svc, db
}

// seedEntity inserts a row so ReEmbedAll can pick it up.
func seedEntity(t *testing.T, db *sql.DB, id, content string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		id, "world", content, []byte{},
	)
	if err != nil {
		t.Fatalf("seed entity %q: %v", id, err)
	}
}

// --- New ---

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	vi := vector.NewInMemoryVectorIndex(db)
	svc := reembed.New(db, vi, stubEmbedder{})
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

// --- ReEmbedAll ---

func TestReEmbedAll_EmptyDB(t *testing.T) {
	svc, _ := newReembedFixture(t)
	result, err := svc.ReEmbedAll(t.Context(), 3, 50, "")
	if err != nil {
		t.Fatalf("ReEmbedAll: %v", err)
	}
	if result.TotalEntities != 0 {
		t.Fatalf("want 0 total entities, got %d", result.TotalEntities)
	}
	if result.ReEmbedded != 0 {
		t.Fatalf("want 0 re-embedded, got %d", result.ReEmbedded)
	}
	if result.Elapsed == "" {
		t.Fatal("elapsed not populated")
	}
}

func TestReEmbedAll_WithEntities(t *testing.T) {
	svc, db := newReembedFixture(t)
	seedEntity(t, db, "re1", "hello world")
	seedEntity(t, db, "re2", "another entity")
	// Seed a row with empty content — should be Skipped.
	seedEntity(t, db, "re3", "")

	result, err := svc.ReEmbedAll(t.Context(), 3, 50, "test-model-v2")
	if err != nil {
		t.Fatalf("ReEmbedAll: %v", err)
	}
	if result.TotalEntities != 3 {
		t.Fatalf("want 3 total entities, got %d", result.TotalEntities)
	}
	if result.Skipped != 1 {
		t.Fatalf("want 1 skipped (empty content), got %d", result.Skipped)
	}
	if result.ReEmbedded != 2 {
		t.Fatalf("want 2 re-embedded, got %d", result.ReEmbedded)
	}
	if result.Failed != 0 {
		t.Fatalf("want 0 failed, got %d", result.Failed)
	}
	if result.Batches == 0 {
		t.Fatal("batches not populated")
	}
	if result.Elapsed == "" {
		t.Fatal("elapsed not populated")
	}
	if result.NewDim != 3 {
		t.Fatalf("want dim=3, got %d", result.NewDim)
	}

	// Verify meta table was updated.
	var modelName string
	if err := db.QueryRow("SELECT value FROM meta WHERE key = 'model_name'").Scan(&modelName); err != nil {
		t.Fatalf("model_name meta: %v", err)
	}
	if modelName != "test-model-v2" {
		t.Fatalf("want model_name=test-model-v2, got %q", modelName)
	}
	var dimStr string
	if err := db.QueryRow("SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&dimStr); err != nil {
		t.Fatalf("embedding_dim meta: %v", err)
	}
	if dimStr != "3" {
		t.Fatalf("want embedding_dim=3, got %q", dimStr)
	}
}

// --- NeedsReEmbed ---

func TestNeedsReEmbed_NoDrift(t *testing.T) {
	svc, _ := newReembedFixture(t)
	needs, oldDim, err := svc.NeedsReEmbed(t.Context(), 3)
	if err != nil {
		t.Fatalf("NeedsReEmbed: %v", err)
	}
	if needs {
		t.Fatal("expected no drift (dims match)")
	}
	if oldDim != 3 {
		t.Fatalf("want oldDim=3, got %d", oldDim)
	}
}

func TestNeedsReEmbed_DriftDetected(t *testing.T) {
	svc, db := newReembedFixture(t)
	// Seed a meta row with dim=2 (INSERT OR REPLACE because
	// store.MemDB → CheckMeta seeds embedding_dim=3 at init).
	_, err := db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('embedding_dim', '2')")
	if err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	needs, oldDim, err := svc.NeedsReEmbed(t.Context(), 3)
	if err != nil {
		t.Fatalf("NeedsReEmbed: %v", err)
	}
	if !needs {
		t.Fatal("expected drift (meta says dim=2, configured dim=3)")
	}
	if oldDim != 2 {
		t.Fatalf("want oldDim=2, got %d", oldDim)
	}
}
