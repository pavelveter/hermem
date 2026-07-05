package ingestion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// TestLoadCheckpointMissingFileReturnsZero covers the first-run path:
// no checkpoint file on disk yet → fresh zero-value stamped with the
// caller-supplied workerID. Verifies the function does not surface an
// error for the expected absence case.
func TestLoadCheckpointMissingFileReturnsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.json")

	got := LoadCheckpoint(path, "worker-A")
	if got.LastCommittedIndex != 0 {
		t.Errorf("fresh start: LastCommittedIndex = %d, want 0", got.LastCommittedIndex)
	}
	if got.WorkerID != "worker-A" {
		t.Errorf("fresh start: WorkerID = %q, want worker-A", got.WorkerID)
	}
	if !got.LastCommittedAt.IsZero() {
		t.Errorf("fresh start: LastCommittedAt = %v, want zero", got.LastCommittedAt)
	}
}

// TestLoadCheckpointCorruptFileFallsBackToFresh covers the resilience
// path: a corrupt JSON file must NOT block worker startup — it falls
// back to a fresh start with the caller's workerID stamped.
func TestLoadCheckpointCorruptFileFallsBackToFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.json")
	if err := os.WriteFile(path, []byte("not-json"), 0644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}

	got := LoadCheckpoint(path, "worker-B")
	if got.WorkerID != "worker-B" {
		t.Errorf("corrupt fall-back: WorkerID = %q, want worker-B", got.WorkerID)
	}
	if got.LastCommittedIndex != 0 {
		t.Errorf("corrupt fall-back: LastCommittedIndex = %d, want 0", got.LastCommittedIndex)
	}
}

// TestLoadCheckpointRoundTrip covers the canonical save -> load path:
// after SaveCheckpoint, LoadCheckpoint returns the same fields.
func TestLoadCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.json")
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	saved := IngestionCheckpoint{
		LastCommittedIndex: 42,
		LastCommittedAt:    now,
		WorkerID:           "worker-XYZ",
	}
	if err := SaveCheckpoint(path, saved); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	loaded := LoadCheckpoint(path, "worker-XYZ")
	if loaded.LastCommittedIndex != 42 {
		t.Errorf("round-trip: LastCommittedIndex = %d, want 42", loaded.LastCommittedIndex)
	}
	if !loaded.LastCommittedAt.Equal(now) {
		t.Errorf("round-trip: LastCommittedAt = %v, want %v", loaded.LastCommittedAt, now)
	}
	if loaded.WorkerID != "worker-XYZ" {
		t.Errorf("round-trip: WorkerID = %q, want worker-XYZ", loaded.WorkerID)
	}
}

// TestSaveCheckpointEmptyPathNoOp covers the opt-out path: when ckptPath
// is empty, SaveCheckpoint returns nil without touching the filesystem.
func TestSaveCheckpointEmptyPathNoOp(t *testing.T) {
	if err := SaveCheckpoint("", IngestionCheckpoint{LastCommittedIndex: 7}); err != nil {
		t.Errorf("empty path SaveCheckpoint: err = %v, want nil", err)
	}
}

// TestSaveCheckpointAtomicRenameLeavesNoTmp covers the atomic write
// invariant: after a successful SaveCheckpoint, the .tmp file MUST be
// gone (renamed away) — never visible side-by-side with the canonical
// path. The rename should be observable to a checker that lists the
// directory immediately after.
func TestSaveCheckpointAtomicRenameLeavesNoTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.json")
	if err := SaveCheckpoint(path, IngestionCheckpoint{LastCommittedIndex: 1, WorkerID: "w"}); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("SaveCheckpoint left .tmp file behind: %s", e.Name())
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected canonical file present after SaveCheckpoint: %v", err)
	}
}

// TestSaveCheckpointConcurrentWritesNoCorruption covers a hazard the
// per-msg save loop exposes: N goroutines calling SaveCheckpoint
// concurrently with different LastCommittedIndex values MUST produce a
// fully-formed JSON file at path. Worst case we observe the latest
// writer's data — never a torn write.
func TestSaveCheckpointConcurrentWritesNoCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.json")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ckpt := IngestionCheckpoint{
				LastCommittedIndex: int64(idx),
				LastCommittedAt:    time.Now().UTC(),
				WorkerID:           "concurrent",
			}
			if err := SaveCheckpoint(path, ckpt); err != nil {
				t.Errorf("concurrent SaveCheckpoint[%d]: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("post-concurrency ReadFile: %v", err)
	}
	var got IngestionCheckpoint
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("post-concurrency Unmarshal: %v (file content: %q)", err, data)
	}
	if got.WorkerID != "concurrent" {
		t.Errorf("post-concurrency WorkerID = %q, want concurrent", got.WorkerID)
	}
	if got.LastCommittedIndex < 0 || got.LastCommittedIndex > 15 {
		t.Errorf("post-concurrency LastCommittedIndex = %d, want 0..15", got.LastCommittedIndex)
	}
}

// TestSavePendingQueueJSONLRoundTrip covers the § 4.2 drain contract:
// the side file is one JSON object per line, and each line is a
// complete core.MemoryMessage that a producer can replay.
func TestSavePendingQueueJSONLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")
	msgs := []core.MemoryMessage{
		{Dialog: "first dialog", ConversationID: "c1", MessageID: "m1"},
		{Dialog: "second dialog", ConversationID: "c1", MessageID: "m2"},
		{Dialog: "third dialog", ConversationID: "c2", MessageID: "m3"},
	}
	if err := SavePendingQueue(path, msgs); err != nil {
		t.Fatalf("SavePendingQueue: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := splitLines(string(data))
	if len(lines) != len(msgs) {
		t.Fatalf("line count = %d, want %d (content: %q)", len(lines), len(msgs), data)
	}
	for i, ln := range lines {
		var got core.MemoryMessage
		if err := json.Unmarshal([]byte(ln), &got); err != nil {
			t.Errorf("line %d: Unmarshal: %v (content: %q)", i, err, ln)
			continue
		}
		if got.Dialog != msgs[i].Dialog {
			t.Errorf("line %d: Dialog = %q, want %q", i, got.Dialog, msgs[i].Dialog)
		}
		if got.ConversationID != msgs[i].ConversationID {
			t.Errorf("line %d: ConversationID = %q, want %q", i, got.ConversationID, msgs[i].ConversationID)
		}
		if got.MessageID != msgs[i].MessageID {
			t.Errorf("line %d: MessageID = %q, want %q", i, got.MessageID, msgs[i].MessageID)
		}
	}
}

// TestSavePendingQueueEmptySlice covers the edge case: an empty slice
// must still create the file (so a reader can distinguish "drained on
// cancel but no pending" from "drain never ran").
func TestSavePendingQueueEmptySlice(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")
	if err := SavePendingQueue(path, nil); err != nil {
		t.Fatalf("SavePendingQueue(nil): %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("empty slice: file content = %q, want empty", data)
	}
}

// TestSavePendingQueueEmptyPathNoOp covers the opt-out path: empty
// pendingPath returns nil without filesystem writes — used by tests
// and any producer that doesn't want replay-on-restart support.
func TestSavePendingQueueEmptyPathNoOp(t *testing.T) {
	if err := SavePendingQueue("", []core.MemoryMessage{{Dialog: "x"}}); err != nil {
		t.Errorf("empty path SavePendingQueue: err = %v, want nil", err)
	}
}

// splitLines is a small helper that breaks a string on '\n' and drops
// any empty trailing entry (json.Encoder always appends a final \n
// after the last record, and we don't want to count that as a record).
func splitLines(s string) []string {
	out := make([]string, 0, 4)
	cur := ""
	for _, r := range s {
		if r == '\n' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// TestSaveCheckpoint_FreshInstall_SmokeMode is a SMOKE test (not a
// correctness canary): for a fresh install, the explicit 0o600 mode
// argument on writeOwnerOnly's WriteFile alone produces a 0o600 file,
// because open(2) consults the mode on file CREATION. This test would
// pass even if the post-WriteFile Chmod were accidentally removed.
// The actual canary is TestSaveCheckpoint_LegacyUpgrade_NarrowsMode
// below — that one distinguishes the post-Chmod from bare WriteFile.
// Both kept: this smoke test catches gross mode regressions; the canary
// catches silent post-Chmod removal. Do NOT drop either as redundant.
func TestSaveCheckpoint_FreshInstall_SmokeMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.json")

	if err := SaveCheckpoint(path, IngestionCheckpoint{
		LastCommittedIndex: 1,
		WorkerID:           "w",
	}); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode after SaveCheckpoint: got %#o, want %#o", got, want)
	}
}

// TestSaveCheckpoint_LegacyUpgrade_NarrowsMode is the CORRECTNESS
// CANARY for the writeOwnerOnly helper. A 0o644 checkpoint file from
// an upgraded-from-0.3.x install MUST be actively narrowed to 0o600 by
// the next SaveCheckpoint call. Plain os.WriteFile does NOT narrow
// (open(2)'s mode argument is only consulted on file creation); the
// post-WriteFile Chmod in writeOwnerOnly is what closes that
// migration gap. If this test fails, the post-Chmod was removed.
func TestSaveCheckpoint_LegacyUpgrade_NarrowsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.json")
	if err := os.WriteFile(path, []byte("legacy 0o644 file"), 0o644); err != nil {
		t.Fatalf("seed legacy 0o644: %v", err)
	}

	if err := SaveCheckpoint(path, IngestionCheckpoint{
		LastCommittedIndex: 2,
		WorkerID:           "w",
	}); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode after upgrade-mutation: got %#o, want %#o", got, want)
	}
}

// TestSavePendingQueue_FreshInstall_SmokeMode is the SMOKE test for
// the pending-queue hardening. For a fresh install, os.Create defaults
// to 0666 (umask narrows to 0o644); the post-Create defer f.Chmod is
// what gets us to 0o600. This test asserts the success path; the
// legacy-upgrade canary below asserts the migration path. Both kept
// for the same reason as the SaveCheckpoint pair.
func TestSavePendingQueue_FreshInstall_SmokeMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")
	msgs := []core.MemoryMessage{
		{Dialog: "test dialog", ConversationID: "c1", MessageID: "m1"},
	}
	if err := SavePendingQueue(path, msgs); err != nil {
		t.Fatalf("SavePendingQueue: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode after SavePendingQueue: got %#o, want %#o", got, want)
	}
}

// TestSavePendingQueue_LegacyUpgrade_NarrowsMode is the CORRECTNESS
// CANARY for the post-Create defer f.Chmod. A 0o644 pending.jsonl from
// an upgraded-from-0.3.x install MUST be narrowed to 0o600 by the next
// SavePendingQueue call regardless of how the original file got its
// mode. The defer f.Chmod(0o600) runs after Create returns, narrowing
// even existing files. If this test fails, the defer-Chmod was
// removed or fell out of the success path.
func TestSavePendingQueue_LegacyUpgrade_NarrowsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")
	if err := os.WriteFile(path, []byte("legacy 0o644 file"), 0o644); err != nil {
		t.Fatalf("seed legacy 0o644: %v", err)
	}

	msgs := []core.MemoryMessage{
		{Dialog: "test dialog", ConversationID: "c1", MessageID: "m1"},
	}
	if err := SavePendingQueue(path, msgs); err != nil {
		t.Fatalf("SavePendingQueue: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode after upgrade-mutation: got %#o, want %#o", got, want)
	}
}
