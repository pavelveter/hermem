package store

import (
	"database/sql"
	"fmt"
	"testing"
)

// TestFindConnectedComponents_ArchivedEntityStillBridges pins the adjacency
// contract: components are seeded only by live entities, but an edge that runs
// *through* an archived entity still joins the live entities on both sides of
// it. Archiving is a visibility filter, not an edge deletion — the vector
// store's warm/verify paths and search all treat archived rows as still
// present, so a bridge that vanished on archive would silently split a
// component the moment one member was archived.
func TestFindConnectedComponents_ArchivedEntityStillBridges(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "a", "world", "a")
	seedEntity(t, db, "bridge", "world", "b")
	seedEntity(t, db, "c", "world", "c")
	seedEntity(t, db, "lonely-archived", "world", "d")
	seedEdge(t, db, "a", "bridge", "uses", 1)
	seedEdge(t, db, "bridge", "c", "uses", 1)
	_, _ = db.Exec(`UPDATE entities SET archived = 1 WHERE id IN ('bridge', 'lonely-archived')`)

	got, err := FindConnectedComponents(db, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want one component ({a,bridge,c}), got %d: %+v", len(got), got)
	}
	if got[0].Size != 3 {
		t.Fatalf("Size: want 3, got %d (%v)", got[0].Size, got[0].IDs)
	}
	// The archived-only pair has no live seed, so it must not be reported.
	for _, id := range got[0].IDs {
		if id == "lonely-archived" {
			t.Fatalf("component reachable only from an archived entity was reported: %v", got[0].IDs)
		}
	}
}

// TestFindConnectedComponents_ArchivedOnlyComponentNotReported covers the other
// half of the same contract: a component whose members are all archived is
// never seeded, so it never appears in the output.
func TestFindConnectedComponents_ArchivedOnlyComponentNotReported(t *testing.T) {
	db := openTestDB(t)
	seedEntity(t, db, "dead-a", "world", "a")
	seedEntity(t, db, "dead-b", "world", "b")
	seedEntity(t, db, "live", "world", "c")
	seedEdge(t, db, "dead-a", "dead-b", "uses", 1)
	_, _ = db.Exec(`UPDATE entities SET archived = 1 WHERE id IN ('dead-a', 'dead-b')`)

	got, err := FindConnectedComponents(db, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Size != 1 || got[0].IDs[0] != "live" {
		t.Fatalf("want only the live singleton, got %+v", got)
	}
}

// BenchmarkFindConnectedComponents guards the dense-index CSR adjacency: the
// component scan reads every entity and edge once, so the benchmark tracks the
// per-row decode cost as well as the BFS.
func BenchmarkFindConnectedComponents(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db := openBenchGraph(b, n, 3)
			defer db.Close()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := FindConnectedComponents(db, 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// openBenchGraph seeds n entities in a ring with fanout extra edges each.
func openBenchGraph(b *testing.B, n, fanout int) *sql.DB {
	b.Helper()
	db, err := MemDBRandom()
	if err != nil {
		b.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := tx.Exec(`INSERT INTO entities (id, category, content) VALUES (?, 'world', 'c')`, fmt.Sprintf("e-%04d", i)); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		for j := 0; j < fanout; j++ {
			dst := (i*fanout + j + 1) % n
			if _, err := tx.Exec(`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, 'uses', 1)`, fmt.Sprintf("e-%04d", i), fmt.Sprintf("e-%04d", dst)); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return db
}
