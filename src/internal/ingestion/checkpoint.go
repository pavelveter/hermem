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
// path is a no-op.
//
// Atomicity: this mirrors SaveCheckpoint's tmp-and-rename pattern. The
// canonical `path` is NEVER in a partial state. We stream into a
// uniquely-suffixed `${path}.tmp.${saveTmpCounter}` (mode 0o600 at
// OpenFile time), fsync, narrow as belt-and-suspenders (covers the
// practically impossible counter-rollover case where .tmp.N might
// alias a stale file), close, then rename atomically. A co-resident
// observer sees either prior content (or ENOENT on first save) or
// full new content at 0o600 — never partial-write garbage. This
// closes the post-Create world-readable window the prior in-place
// os.Create + Sync + Chmod design had.
// Empty slice yields an empty file (no truncation races on the .tmp
// side, since OpenFile + Encode-loop writes zero records).
func SavePendingQueue(path string, msgs []core.MemoryMessage) error {
	if path == "" {
		return nil
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, saveTmpCounter.Add(1))
	if err := writePendingTmp(tmp, msgs); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup; mirrors SaveCheckpoint
		return fmt.Errorf("pending: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup; mirrors SaveCheckpoint
		return fmt.Errorf("pending: rename: %w", err)
	}
	return nil
}

// writePendingTmp streams msgs as JSONL into path at mode 0o600,
// fsyncs, then narrows to 0o600 as belt-and-suspenders. It is the
// per-save streaming companion to writeOwnerOnly: same atomicity
// intent, different write surface (streaming encoder instead of
// fully-buffered bytes). The named return lets the deferred f.Close
// propagate any final-flush error to the caller while preserving the
// function's primary error from earlier returns. On error, the
// caller benchmarks the .tmp file away (best-effort Remove) before
// returning the wrapped error.
func writePendingTmp(path string, msgs []core.MemoryMessage) (err error) {
	// O_TRUNC handles the (impossibly rare) counter-rollover collision
	// where .tmp.N might alias a stale file from a prior run; the
	// 0o600 mode arg applies at file CREATION. The post-Sync f.Chmod
	// below is belt-and-suspenders for the rollover-aliased case
	// where O_CREAT's mode arg does NOT reach an existing inode.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// Defer close; on success, capture any final-flush Close error into
	// the named return. On error paths, the named err is already set
	// by an earlier `return …` — do not overwrite.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	// Belt-and-suspenders narrowing. Fresh tmp: open(2) mode arg
	// already creates at 0o600, so this is a no-op. Rollover case:
	// narrows the inherited mode (theoretically 0o644 if .tmp.N
	// re-uses a stale path) before Rename — ensures Rename moves an
	// inode at 0o600.
	if err := f.Chmod(0o600); err != nil {
		return err
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
// actively narrows the legacy case. SaveCheckpoint uses this helper;
// SavePendingQueue uses the streaming companion writePendingTmp above
// (same atomicity intent, different write surface). Both helpers
// converge on the same owner-only invariant.
func writeOwnerOnly(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
