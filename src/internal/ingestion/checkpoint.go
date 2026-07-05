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
	if err := writeOwnerOnly(tmp, data); err != nil {
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
func SavePendingQueue(path string, msgs []core.MemoryMessage) (err error) {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("pending: create: %w", err)
	}
	defer f.Close()
	// Belt-and-suspenders narrowing under two distinct contracts:
	//   - On SUCCESS paths, Chmod errors must propagate so the caller
	//     can escalate. The inline post-Sync f.Chmod below carries that.
	//   - On ERROR paths (enc.Encode / f.Sync), the residual content
	//     (partial or complete) MUST still be narrowed to 0o600 before
	//     close, otherwise a co-resident process can read the failure-
	//     mode snapshot. This defer Chmod is best-effort because the
	//     primary error path has already returned — surfacing a SECOND
	//     error would mask the primary. Defer registered AFTER defer
	//     Close so it runs first via LIFO (on the still-open fd).
	defer func() {
		if err == nil {
			return
		}
		_ = f.Chmod(0o600)
	}()
	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			return fmt.Errorf("pending: encode: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("pending: sync: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("pending: chmod 0o600: %w", err)
	}
	return nil
}

// defaultDrainTimeout caps a ctx-cancel drain so a producer that does
// not close its channel cannot stall MemoryWorkerResilient indefinitely.
// Tiny value because the intent is "flush before exit"; a
// 5s ceiling comfortably covers in-flight embedding/LLM calls in normal
// operation.
const defaultDrainTimeout = 5 * time.Second

// writeOwnerOnly writes data to path with mode 0o600 AND issues an
// os.Chmod(0o600). Plain os.WriteFile is NOT enough: open(2)'s mode
// argument only applies on file CREATION, so a pre-existing 0o644
// checkpoint (e.g. an upgraded-from-0.3.x install) would stay 0o644
// after a WriteFile+truncate. The post-WriteFile Chmod is what
// actively narrows the legacy case. SaveCheckpoint routes through this
// helper; the parallel SavePendingQueue uses f.Chmod(0o600) directly
// because it streams via os.Create (the buffer-then-WriteFile pattern
// would re-allocate the message slice). Both paths converge on the
// same owner-only invariant.
func writeOwnerOnly(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
