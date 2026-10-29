package store

import (
	"strings"
	"testing"
)

// TestEntityLookupIndexes covers the two scans migration
// 015_idx_entity_lookups.sql was added to remove. Both queries are
// correctness-neutral, so the test asserts on the QUERY PLAN: if the index is
// dropped or the query is rewritten to stop matching it, SQLite falls back to
// a full table SCAN and this test fails.
func TestEntityLookupIndexes(t *testing.T) {
	db := openTestDB(t)

	plans := map[string]string{
		"provenance": `SELECT id FROM entities WHERE archived = 0 AND (conversation_id = 'x') ORDER BY created_at DESC LIMIT 50`,
		"retention":  `SELECT id FROM entities WHERE category = 'observation' AND updated_at < '2020-01-01 00:00:00' AND archived = 0 LIMIT 100`,
	}
	wantIndex := map[string]string{
		"provenance": "idx_entities_conversation_id_created_at",
		"retention":  "idx_entities_category_updated_at",
	}

	for name, query := range plans {
		rows, err := db.Query("EXPLAIN QUERY PLAN " + query)
		if err != nil {
			t.Fatalf("%s: explain: %v", name, err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				rows.Close()
				t.Fatalf("%s: scan plan: %v", name, err)
			}
			details = append(details, detail)
		}
		rows.Close()
		joined := strings.Join(details, " | ")
		if !strings.Contains(joined, "SEARCH") || !strings.Contains(joined, wantIndex[name]) {
			t.Fatalf("%s: want a SEARCH using %s, got plan: %s", name, wantIndex[name], joined)
		}
	}
}
