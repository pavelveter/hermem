package ingestion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// TestSavePendingQueue_NoOrphanTmpOnSuccess covers the atomicity
// guarantee of the write-tmp-then-rename pattern that SavePendingQueue
// adopted to mirror SaveCheckpoint. After a successful SavePendingQueue,
// the directory listing MUST contain no `.tmp` or `.tmp.N` residue.
// The Rename step either succeeded (the tmp inode is renamed away;
// the directory entry is reused for the new canonical filename) or
// failed (the caller cleanups via best-effort Remove). A passing save
// leaves zero `.tmp` artefacts behind.
func TestSavePendingQueue_NoOrphanTmpOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")

	msgs := []core.MemoryMessage{
		{Dialog: "first", ConversationID: "c1", MessageID: "m1"},
	}
	if err := SavePendingQueue(path, msgs); err != nil {
		t.Fatalf("SavePendingQueue: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		// Match `pending.jsonl.tmp.N` (suffix `.tmp.\\d+`) — i.e. any
		// file whose name contains `.tmp.` followed by digits.
		name := e.Name()
		if strings.Contains(name, ".tmp.") || strings.HasSuffix(name, ".tmp") {
			t.Errorf("SavePendingQueue left tmp residue behind: %s", name)
		}
	}
}

// TestSavePendingQueue_AtomicRenameReplacesPreExisting asserts that a
// pre-existing pending.jsonl is REPLACED (not appended-to, not merged)
// by the JSONL SavePendingQueue produces. This is the heart of the
// atomicity invariant: a co-resident reader at any point sees either
// the prior content (pre-call) or the final new content (post-call),
// never a concatenation of the two. Confirming this empirically also
// guards against regressions where the rename step is silently
// replaced with an in-place truncate-write.
func TestSavePendingQueue_AtomicRenameReplacesPreExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")

	// Seed a prior 0o644 pending file. The pre-existing content MUST
	// not survive the subsequent SavePendingQueue call.
	prior := `{"Dialog":"PRIOR_A","ConversationID":"legacy","MessageID":"a"}
{"Dialog":"PRIOR_B","ConversationID":"legacy","MessageID":"b"}
`
	if err := os.WriteFile(path, []byte(prior), 0o644); err != nil {
		t.Fatalf("seed prior pending.jsonl: %v", err)
	}

	// New save: a completely different content slice.
	newMsgs := []core.MemoryMessage{
		{Dialog: "NEW", ConversationID: "fresh", MessageID: "n1"},
		{Dialog: "NEW2", ConversationID: "fresh", MessageID: "n2"},
	}
	if err := SavePendingQueue(path, newMsgs); err != nil {
		t.Fatalf("SavePendingQueue: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// ATOMICITY CHECK: a co-resident reader must not observe any
	// concatenation of prior + new. The post-save file content
	// contains absolutely no prior records.
	if strings.Contains(string(data), "PRIOR_") {
		t.Errorf("post-rename pending.jsonl still contains prior content — atomicity violated:\n%s", data)
	}
	if strings.Contains(string(data), "legacy") {
		t.Errorf("post-rename pending.jsonl still contains prior ConversationID — atomicity violated:\n%s", data)
	}

	// The new records MUST be present, in order, as valid JSONL.
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != len(newMsgs) {
		t.Fatalf("post-rename line count = %d, want %d (content: %q)", len(lines), len(newMsgs), data)
	}
	var got []core.MemoryMessage
	for i, line := range lines {
		var m core.MemoryMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("post-rename line %d: Unmarshal: %v (content: %q)", i, err, line)
			continue
		}
		got = append(got, m)
	}
	if len(got) != len(newMsgs) {
		t.Fatalf("post-rename parsed count = %d, want %d", len(got), len(newMsgs))
	}
	for i, m := range got {
		if m.Dialog != newMsgs[i].Dialog {
			t.Errorf("post-rename line %d: Dialog = %q, want %q", i, m.Dialog, newMsgs[i].Dialog)
		}
		if m.ConversationID != newMsgs[i].ConversationID {
			t.Errorf("post-rename line %d: ConversationID = %q, want %q", i, m.ConversationID, newMsgs[i].ConversationID)
		}
	}

	// Mode narrowing still applies: legacy 0o644 → 0o600 on the next
	// save. Mirrors TestSavePendingQueue_LegacyUpgrade_NarrowsMode.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if m := info.Mode().Perm(); m != 0o600 {
		t.Errorf("mode after atomic-rename: got %#o, want 0o600", m)
	}
}

// TestSavePendingQueueConcurrentWritesNoCorruption is the
// concurrency correctness canary for the atomic-rename pattern.
// N goroutines call SavePendingQueue concurrently with different
// content slices. After wait, the canonical path MUST hold a
// fully-formed JSONL matching exactly one contributor's slice (the
// last writer wins by atomic-rename; intermediate partial states are
// never visible). The test asserts:
//   - ReadFile succeeds (no torn write).
//   - The line count matches exactly one of the input slice lengths.
//   - Every line parses as a valid MemoryMessage.
func TestSavePendingQueueConcurrentWritesNoCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.jsonl")

	const N = 16
	// Vary slice sizes 1..5 across the 16 goroutines; last writer
	// wins by atomic-rename, so the post-concurrency line count
	// must be one of {1,2,3,4,5}.
	inputs := make([]int, N)
	for i := 0; i < N; i++ {
		inputs[i] = (i % 5) + 1
	}

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msgs := make([]core.MemoryMessage, inputs[idx])
			for j := range msgs {
				msgs[j] = core.MemoryMessage{
					Dialog:         "msg-" + string(rune('a'+idx)) + "-" + string(rune('0'+j)),
					ConversationID: "concurrent",
					MessageID:      "m-" + string(rune('a'+idx)) + "-" + string(rune('0'+j)),
				}
			}
			if err := SavePendingQueue(path, msgs); err != nil {
				t.Errorf("concurrent SavePendingQueue[%d]: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("post-concurrency ReadFile: %v", err)
	}
	// JSONL is one record per line + trailing \n from json.Encoder.
	// Trim the trailing newline to count records.
	body := strings.TrimRight(string(data), "\n")
	if body == "" {
		t.Fatalf("post-concurrency: empty pending.jsonl after concurrent writes")
	}
	lines := strings.Split(body, "\n")
	if len(lines) < 1 {
		t.Fatalf("post-concurrency: zero lines")
	}
	// Line count must match one of the input sizes (1..5 froms the
	// (i%5)+1 formula). Torn writes would produce malformed JSON or a
	// mismatched line count.
	valid := false
	for _, in := range inputs {
		if len(lines) == in {
			valid = true
			break
		}
	}
	if !valid {
		t.Errorf("post-concurrency line count = %d, want one of: %v (content: %q)", len(lines), inputs, body)
	}
	// Every line parses as a valid MemoryMessage — guards against
	// torn-write data inside any single record.
	for i, line := range lines {
		var m core.MemoryMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("post-concurrency line %d: Unmarshal: %v (content: %q)", i, err, line)
		}
	}
}
