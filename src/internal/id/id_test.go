package id

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNewTaskID_Grammar pins the ADR-035 format: "task-" + 26 Crockford
// base32 chars (ULID). The legacy "task-<counter>" format is gone; its
// process-local collision risk was the ADR's motivating bug.
func TestNewTaskID_Grammar(t *testing.T) {
	got := NewTaskID()
	if !strings.HasPrefix(got, "task-") {
		t.Fatalf("want task- prefix, got %q", got)
	}
	if len(got) != len("task-")+26 {
		t.Fatalf("want 26-char body, got %q (%d)", got, len(got))
	}
	if !Validate(got) {
		t.Fatalf("Validate rejected generated ID %q", got)
	}
}

func TestNewTaskID_UniqueUnderConcurrency(t *testing.T) {
	const n = 10_000
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range n / 8 {
				ids <- NewTaskID()
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]struct{}, n)
	for id := range ids {
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ID %q across goroutines", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("want %d unique IDs, got %d", n, len(seen))
	}
}

// TestNewTaskID_TimeOrdered verifies that IDs minted in later
// milliseconds sort after earlier ones (ULID timestamp prefix).
func TestNewTaskID_TimeOrdered(t *testing.T) {
	early := NewTaskID()
	time.Sleep(5 * time.Millisecond)
	later := NewTaskID()
	if early >= later {
		t.Fatalf("time ordering broken: early=%q later=%q", early, later)
	}
}

func TestInspect_TaskID_DecodesTimestamp(t *testing.T) {
	before := time.Now().Add(-time.Second)
	got := NewTaskID()
	after := time.Now().Add(time.Second)

	info, err := Inspect(got)
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "task" || info.Time == "" {
		t.Fatalf("info = %+v", info)
	}
	ts, err := time.Parse(time.RFC3339, info.Time)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Before(before) || ts.After(after) {
		t.Fatalf("decoded time %v outside [%v, %v]", ts, before, after)
	}
}

// TestContentEntityID_Deterministic pins ADR-035 decision 2: same
// normalized content + category ⇒ same ID, regardless of case or
// whitespace noise.
func TestContentEntityID_Deterministic(t *testing.T) {
	a := ContentEntityID("world", "Pavel likes   coffee")
	b := ContentEntityID("world", "pavel likes coffee")
	if a != b {
		t.Fatalf("normalized inputs diverged: %q vs %q", a, b)
	}
	c := ContentEntityID("world", "pavel likes coffee")
	if a != c {
		t.Fatalf("non-deterministic: %q vs %q", a, c)
	}
	if !strings.HasPrefix(a, "ent-") || len(a) != len("ent-")+26 {
		t.Fatalf("bad shape: %q", a)
	}
	if !Validate(a) {
		t.Fatalf("Validate rejected %q", a)
	}
}

func TestContentEntityID_ScopeSensitivity(t *testing.T) {
	base := ContentEntityID("world", "same content")
	otherCategory := ContentEntityID("person", "same content")
	if base == otherCategory {
		t.Fatal("category must participate in the hash")
	}
	info, err := Inspect(base)
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "ent" || info.Time != "" {
		t.Fatalf("ent IDs carry no clock: %+v", info)
	}
}

func TestInspect_RejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "task-1", "task-", "ent-short", "nope-0123456789ABCDEFGHJKMNPQ", "task-0123456789ABCDEFGHIJKLMNOP"} {
		if _, err := Inspect(bad); !errors.Is(err, ErrBadID) {
			t.Errorf("Inspect(%q) err = %v, want ErrBadID", bad, err)
		}
		if Validate(bad) {
			t.Errorf("Validate(%q) = true, want false", bad)
		}
	}
}

func TestNormalizeContent_VersionLock(t *testing.T) {
	// Pin the v1 normalizer exactly: NFC, lowercase, collapsed internal
	// whitespace, trimmed edges. Changing behavior requires bumping
	// contentScheme, so this test guards accidental drift.
	got := normalizeContent("  Ünïcode\t\tTEXT\r\nwith   spaces  ")
	want := "ünïcode text with spaces"
	if got != want {
		t.Fatalf("normalizeContent v1 drift: got %q want %q", got, want)
	}
}
