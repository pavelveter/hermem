package ingestion

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// saveTmpCounter assigns each SaveCheckpoint call a unique .tmp.N
// suffix so the rename step never observes a stale tmp that a
// concurrent caller just moved away. Without this, two goroutines
// writing the SAME path + ".tmp" can race on the rename path: one
// wins, the other gets ENOENT because the previous rename already
// moved the source tmp → path.
var saveTmpCounter atomic.Uint64

// IngestionCheckpoint is the resumability stamp a MemoryWorkerResilient
// instance writes after every successful ProcessDialogWithProvenance.
// On restart, a producer can read this file and skip the first
// `LastCommittedIndex` messages (at-least-once semantics over the
// producer's input channel; the worker itself is idempotent on dedup).
type IngestionCheckpoint struct {
	LastCommittedIndex int64     `json:"last_committed_index"`
	LastCommittedAt    time.Time `json:"last_committed_at"`
	WorkerID           string    `json:"worker_id"`
}

// LoadCheckpoint reads the checkpoint at path. Missing file → fresh
// start; corrupt file → WARN + fresh start (never fails the worker).
// `workerID` is stamped on the result so subsequent saves carry the
// identity that produced them.
func LoadCheckpoint(path, workerID string) IngestionCheckpoint {
	if path == "" {
		return IngestionCheckpoint{WorkerID: workerID}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("checkpoint: read failed, starting fresh", "path", path, "err", err)
		}
		return IngestionCheckpoint{WorkerID: workerID}
	}
	var ckpt IngestionCheckpoint
	if err := json.Unmarshal(data, &ckpt); err != nil {
		slog.Warn("checkpoint: corrupt JSON, falling back to fresh start", "path", path, "err", err)
		return IngestionCheckpoint{WorkerID: workerID}
	}
	// If the on-disk WorkerID is empty (very old checkpoint) or differs,
	// trust the caller's identity so a worker swap is auditable.
	if ckpt.WorkerID == "" {
		ckpt.WorkerID = workerID
	}
	return ckpt
}

// SaveCheckpoint writes the checkpoint atomically via a uniquely-suffixed
// tmp-file + rename so (a) a crash mid-write cannot leave a partial
// file behind, AND (b) concurrent callers never collide on the same
// tmp file. Empty path is a no-op (used by tests that opt out of
// persistence).
//
// Each call gets tmp = "${path}.tmp.${saveTmpCounter}" so two goroutines
// running SaveCheckpoint for the same logical path can NEVER observe
// each other's tmp. The final rename is atomic on POSIX so concurrent
// renames converge on the last writer's full JSON (no torn writes,
// even if a goroutine's path forward is "out of date" w.r.t. another
// goroutine's LastCommittedIndex).
func SaveCheckpoint(path string, ckpt IngestionCheckpoint) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(ckpt, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint: marshal: %w", err)
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, saveTmpCounter.Add(1))
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("checkpoint: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup so an orphan .tmp.N doesn't accumulate
		// across thousands of failed saves.
		_ = os.Remove(tmp)
		return fmt.Errorf("checkpoint: rename: %w", err)
	}
	return nil
}

// SavePendingQueue writes the unprocessed channel items the worker
// drained on ctx-cancel as JSONL. Each line is a single
// core.MemoryMessage — the producer can replay them on restart. Empty
// path is a no-op. Empty slice yields an empty file (no truncation
// races on the writer side).
func SavePendingQueue(path string, msgs []core.MemoryMessage) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("pending: create: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			return fmt.Errorf("pending: encode: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("pending: sync: %w", err)
	}
	return nil
}

// defaultDrainTimeout caps a ctx-cancel drain so a producer that does
// not close its channel cannot stall MemoryWorkerResilient indefinitely.
// Tiny value because the intent is "best-effort flush before exit"; a
// 5s ceiling comfortably covers in-flight embedding/LLM calls in normal
// operation.
const defaultDrainTimeout = 5 * time.Second
