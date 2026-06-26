package diagnose

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("MemDBRandom: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedEntity(t *testing.T, db *sql.DB, id, category, content string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`, id, category, content)
	if err != nil {
		t.Fatalf("seed entity %s: %v", id, err)
	}
}

func seedEntityWithEmb(t *testing.T, db *sql.DB, id, category, content string, emb []float32) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO entities (id, category, content, embedding) VALUES (?, ?, ?, ?)`, id, category, content, store.EmbeddingToBytes(emb))
	if err != nil {
		t.Fatalf("seed entity %s: %v", id, err)
	}
}

func seedEdge(t *testing.T, db *sql.DB, src, dst, rel string) {
	t.Helper()
	_, err := db.Exec(`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES (?, ?, ?)`, src, dst, rel)
	if err != nil {
		t.Fatalf("seed edge %s->%s: %v", src, dst, err)
	}
}

// TestReport_JSON verifies that Report serializes to valid JSON.
func TestReport_JSON(t *testing.T) {
	r := &Report{
		Schema: SchemaReport{
			ForeignKeysOK: true,
			IntegrityOK:   true,
		},
		Vector: VectorReport{
			TotalRows: 42,
			ConfigDim: 768,
			StoredDim: 768,
		},
		Memory: MemoryReport{
			TotalEntities:         10,
			EntitiesWithEmbedding: 8,
			EmbeddingDensity:      80,
			DensityByCategory:     map[string]float64{"world": 100, "observation": 50},
			BeliefCounts:          map[string]int{"Active": 5, "Superseded": 1},
		},
		Retention: RetentionReport{
			ArchivedEntities: 2,
			TotalEntities:    10,
			ArchivedPct:      20,
		},
		Errors: ErrorsReport{
			Note: "slog ERROR entries not persisted.",
		},
	}

	raw := r.JSON()
	var decoded Report
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if decoded.Schema.ForeignKeysOK != true {
		t.Errorf("expected ForeignKeysOK=true, got %v", decoded.Schema.ForeignKeysOK)
	}
	if decoded.Vector.TotalRows != 42 {
		t.Errorf("expected TotalRows=42, got %d", decoded.Vector.TotalRows)
	}
	if decoded.Memory.EmbeddingDensity != 80 {
		t.Errorf("expected EmbeddingDensity=80, got %f", decoded.Memory.EmbeddingDensity)
	}
	if decoded.Retention.ArchivedEntities != 2 {
		t.Errorf("expected ArchivedEntities=2, got %d", decoded.Retention.ArchivedEntities)
	}
}
