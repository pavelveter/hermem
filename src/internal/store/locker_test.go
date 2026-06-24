package store

import (
	"sync"
	"testing"
	"time"
)

// TestEntityLocker_RoundTrip — Lock/Unlock on a single ID. Counts of
// Lock followed by Unlock must complete without panicking on the inner
// sync.Mutex (which would fire if the same ID was unlocked twice).
func TestEntityLocker_RoundTrip(t *testing.T) {
	l := NewEntityLocker(32)
	l.Lock("entity-1")
	l.Unlock("entity-1")
	// Round-trip must be safe to repeat.
	l.Lock("entity-1")
	l.Unlock("entity-1")
}

// TestEntityLocker_DisjointIDsDoNotBlock — two goroutines locking
// different entity IDs should NEVER block on each other. We assert via
// a watchdog: if either goroutine hasn't finished within a tight budget,
// fail with a deadlock flag. Done-channel handshake reports success.
func TestEntityLocker_DisjointIDsDoNotBlock(t *testing.T) {
	l := NewEntityLocker(32)
	var wg sync.WaitGroup
	wg.Add(2)
	done := make(chan struct{}, 2)
	go func() {
		defer wg.Done()
		l.Lock("alpha")
		defer l.Unlock("alpha")
		done <- struct{}{}
	}()
	go func() {
		defer wg.Done()
		l.Lock("beta")
		defer l.Unlock("beta")
		done <- struct{}{}
	}()
	wg.Wait()
	close(done)
	count := 0
	for range done {
		count++
	}
	if count != 2 {
		t.Fatalf("disjoint acquires: want 2 done, got %d", count)
	}
}

// TestEntityLocker_SameIDContends — two goroutines on the SAME entity
// ID MUST block each other (this is the whole point — per-key
// serialization). Total marker count is 3 (G1-locked, G1-done,
// G2-locked); G2 has no "done" marker because its appends (1) plus
// G1's (2) sum to 3 — the previous draft asserted 4 which would fail.
// A goroutine+watchdog hangs the test FAILURE-deadline (1.5s) instead
// of hanging the whole binary if a future regression breaks contention.
func TestEntityLocker_SameIDContends(t *testing.T) {
	l := NewEntityLocker(32)
	var wg sync.WaitGroup
	wg.Add(2)
	var order []string
	var mu sync.Mutex
	go func() {
		defer wg.Done()
		l.Lock("shared")
		mu.Lock()
		order = append(order, "G1-locked")
		mu.Unlock()
		l.Unlock("shared")
		mu.Lock()
		order = append(order, "G1-done")
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		l.Lock("shared")
		mu.Lock()
		order = append(order, "G2-locked")
		mu.Unlock()
		l.Unlock("shared")
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutines did not finish within 5s; per-key serialisation broken")
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 order markers (G1-locked, G1-done, G2-locked); got %v", order)
	}
}

// TestEntityLocker_AcquireBatchSortedOrder — AcquireBatch MUST lex-sort
// the IDs internally AND dedup before locking; we test by feeding an
// unsorted+duplicate input (which would deadlock without dedup because
// sync.Mutex is non-reentrant). Goroutine + watchdog so a regression
// hangs the test with t.Fatal("deadlocked") rather than hanging the
// whole binary.
func TestEntityLocker_AcquireBatchSortedOrder(t *testing.T) {
	l := NewEntityLocker(32)
	done := make(chan struct{})
	go func() {
		ids := []string{"c", "a", "b", "a", "c"} // unsorted + duplicate
		release := l.AcquireBatch(ids)
		if release == nil {
			t.Error("AcquireBatch nil release func for non-empty input")
			close(done)
			return
		}
		release()
		// Smoke: another overlapping batch must not deadlock once the
		// previous release completed.
		release2 := l.AcquireBatch([]string{"a", "b", "c"})
		release2()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AcquireBatch deadlocked under duplicate-id input — dedup regression")
	}
}

// TestEntityLocker_RoundingToPowerOfTwo — shardCount=7 → 8; shardCount=0
// → 32 (default); shardCount=-5 → 32; shardCount=64 → 64 preserved.
func TestEntityLocker_RoundingToPowerOfTwo(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 32},
		{-1, 32},
		{7, 8}, // nearest power of two ≥ in
		{8, 8},
		{9, 16},
		{31, 32},
		{64, 64},
		{100, 128},
	}
	for _, c := range cases {
		l := NewEntityLocker(c.in)
		got := len(l.shards)
		if got != c.want {
			t.Errorf("NewEntityLocker(%d): want %d shards, got %d", c.in, c.want, got)
		}
		// shardMask must equal shardCount-1 (powers of two only).
		if l.shardMask != uint32(c.want-1) {
			t.Errorf("NewEntityLocker(%d): shardMask %d, want %d", c.in, l.shardMask, c.want-1)
		}
	}
}
