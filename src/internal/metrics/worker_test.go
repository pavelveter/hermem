package metrics

import (
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/store"
)

// TestFlushTouchesEveryBufferedEntity covers the batched last_accessed_at
// update: one statement per flush must still stamp every buffered ID, and a
// successful flush must empty the buffer so the same rows are not rewritten
// on the next tick.
func TestFlushTouchesEveryBufferedEntity(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	defer db.Close()

	ids := []string{"m-a", "m-b", "m-c"}
	for _, id := range ids {
		if _, err := db.Exec(`INSERT INTO entities (id, category, content, last_accessed_at) VALUES (?, 'world', 'c', '2000-01-01 00:00:00')`, id); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	w := NewAsyncMetricsWorker(db, 16, 8, time.Hour)
	for _, id := range ids {
		w.Touch(id)
	}
	w.flush()

	for _, id := range ids {
		var at string
		if err := db.QueryRow(`SELECT last_accessed_at FROM entities WHERE id = ?`, id).Scan(&at); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if at == "2000-01-01 00:00:00" {
			t.Errorf("%s: last_accessed_at was not updated", id)
		}
	}

	w.mu.Lock()
	buffered := len(w.buffer)
	w.mu.Unlock()
	if buffered != 0 {
		t.Errorf("buffer still holds %d ids after a successful flush", buffered)
	}
}
