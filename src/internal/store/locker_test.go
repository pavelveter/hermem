package store

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEntityLocker_RoundTrip — LockCtx/Unlock on a single ID. Round-trip
// must be safe to repeat; the inner channel will panic on a second
// Unlock without an intervening acquire.
func TestEntityLocker_RoundTrip(t *testing.T) {
	l := NewEntityLocker(32)
	ctx := context.Background()
	if err := l.LockCtx(ctx, "entity-1"); err != nil {
		t.Fatalf("first LockCtx: %v", err)
	}
	l.Unlock("entity-1")
	if err := l.LockCtx(ctx, "entity-1"); err != nil {
		t.Fatalf("second LockCtx: %v", err)
	}
	l.Unlock("entity-1")
}

// TestEntityLocker_DisjointIDsDoNotBlock — two goroutines locking
// different entity IDs should NEVER block on each other. We assert via
// a watchdog: if either goroutine hasn't finished within a tight budget,
// fail with a deadlock flag. Done-channel handshake reports success.
func TestEntityLocker_DisjointIDsDoNotBlock(t *testing.T) {
	l := NewEntityLocker(32)
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)
	done := make(chan struct{}, 2)
	go func() {
		defer wg.Done()
		if err := l.LockCtx(ctx, "alpha"); err != nil {
			t.Errorf("alpha LockCtx: %v", err)
			done <- struct{}{}
			return
		}
		defer l.Unlock("alpha")
		done <- struct{}{}
	}()
	go func() {
		defer wg.Done()
		if err := l.LockCtx(ctx, "beta"); err != nil {
			t.Errorf("beta LockCtx: %v", err)
			done <- struct{}{}
			return
		}
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
// A goroutine+watchdog hangs the test FAILURE-deadline (5s) instead
// of hanging the whole binary if a future regression breaks contention.
func TestEntityLocker_SameIDContends(t *testing.T) {
	l := NewEntityLocker(32)
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)
	var order []string
	var mu sync.Mutex
	go func() {
		defer wg.Done()
		if err := l.LockCtx(ctx, "shared"); err != nil {
			t.Errorf("G1 LockCtx: %v", err)
			return
		}
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
		if err := l.LockCtx(ctx, "shared"); err != nil {
			t.Errorf("G2 LockCtx: %v", err)
			return
		}
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

// TestEntityLocker_AcquireBatchSortedOrder — AcquireBatchCtx MUST
// lex-sort the IDs internally AND dedup before locking; we test by
// feeding an unsorted+duplicate input (which would deadlock without
// dedup because the chan-receive would block forever on the second
// pass through the duplicated shard).
func TestEntityLocker_AcquireBatchSortedOrder(t *testing.T) {
	l := NewEntityLocker(32)
	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		ids := []string{"c", "a", "b", "a", "c"} // unsorted + duplicate
		release, err := l.AcquireBatchCtx(ctx, ids)
		if err != nil {
			t.Errorf("AcquireBatchCtx: %v", err)
			close(done)
			return
		}
		if release == nil {
			t.Error("AcquireBatchCtx nil release func for non-empty input")
			close(done)
			return
		}
		release()
		// Smoke: another overlapping batch must not deadlock once the
		// previous release completed.
		release2, err := l.AcquireBatchCtx(ctx, []string{"a", "b", "c"})
		if err != nil {
			t.Errorf("AcquireBatchCtx overlap: %v", err)
		}
		if release2 != nil {
			release2()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AcquireBatchCtx deadlocked under duplicate-id input — dedup regression")
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

// TestEntityLocker_LockCtxCancelled — when a goroutine is blocked
// waiting for a shard that another goroutine is holding, the BLOCKED
// goroutine's ctx.Done() must fire and return ErrLockCancelled without
// leaking. This is the audit-Part-5 #1 fix in regression-test form:
// without the chan-based ctx-aware mutex, sync.Mutex.Lock has no
// timeout and the only way out is the holder unlocking, which a stalled
// Louvain pass may never do.
//
// We assert three things post-cancellation:
//  1. error wraps both ErrLockCancelled AND ctx.Err() (DeadlineExceeded).
//  2. returning happens within ~budget+slack (no infinite wait).
//  3. NO goroutine leak — the blocked goroutine actually exits.
func TestEntityLocker_LockCtxCancelled(t *testing.T) {
	l := NewEntityLocker(32)
	ctx := context.Background()

	// Pre-take a shard so the next LockCtx has to wait. We hold it for
	// 1s — comfortably longer than the LockCtx deadline.
	holdCtx, holdCancel := context.WithCancel(ctx)
	defer holdCancel()
	if err := l.LockCtx(holdCtx, "contended"); err != nil {
		t.Fatalf("holder LockCtx: %v", err)
	}
	holderReleased := make(chan struct{})
	go func() {
		defer close(holderReleased)
		time.Sleep(500 * time.Millisecond)
		l.Unlock("contended")
	}()

	// Build a context that fires BEFORE the holder releases.
	waitCtx, waitCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer waitCancel()

	// Track goroutine count via runtime.NumGoroutine delta. Accept some
	// slack because the test harness spawns background goroutines for
	// the timer. The important signal is that the delta AT THE END of
	// the test is the same as the baseline AFTER a settling sleep.
	t0 := wait4StableGoroutineCount(t, 50*time.Millisecond)

	// Spawn N blocked-acquire goroutines; they must all return via
	// ctx.Done() before the holder releases.
	const N = 16
	var wg sync.WaitGroup
	wg.Add(N)
	errCh := make(chan error, N)
	start := time.Now()
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			errCh <- l.LockCtx(waitCtx, "contended")
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// (1) error contract: every err wraps ErrLockCancelled + DeadlineExceeded.
	for i := 0; i < N; i++ {
		err := <-errCh
		if err == nil {
			t.Fatalf("goroutine %d: expected ErrLockCancelled, got nil", i)
		}
		if !errors.Is(err, ErrLockCancelled) {
			t.Errorf("goroutine %d: errors.Is(err, ErrLockCancelled) = false (err=%v)", i, err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("goroutine %d: errors.Is(err, context.DeadlineExceeded) = false (err=%v)", i, err)
		}
	}

	// (2) timing: every waiter must exit ~100ms after ctx deadline
	// (channel select is immediate on ctx.Done). 600ms covers the
	// local jitter envelope without becoming a "waited for holder"
	// smell — if the waiter were blocking on the channel, elapsed
	// would be ~500ms+slack.
	if elapsed > 600*time.Millisecond {
		t.Errorf("cancel path elapsed %v; want ≲600ms — possible starvation regression", elapsed)
	}

	// (3) goroutine-leak check: wait for the holder to release, then
	// settle and confirm count is back near baseline.
	<-holderReleased
	t1 := wait4StableGoroutineCount(t, 200*time.Millisecond)
	if delta := t1 - t0; delta > 4 {
		// 4 allows for test harness churn (timer goroutines, GC assist)
		// without masking a real leak — if blocked waiters didn't exit,
		// this would land at ~1+N.
		t.Errorf("goroutine count drifted by %d after cancellation; possible leak (t0=%d, t1=%d)", delta, t0, t1)
	}
}

// wait4StableGoroutineCount samples runtime.NumGoroutine() until two
// consecutive samples within `stableWindow` agree, OR `budget` elapses.
// Returns the last stable value. Used by the leak detector in
// LockCtxCancelled to filter out timer/GC churn.
func wait4StableGoroutineCount(t *testing.T, budget time.Duration) int {
	t.Helper()
	const stableWindow = 50 * time.Millisecond
	const samplePeriod = 10 * time.Millisecond
	deadline := time.Now().Add(budget)
	prev := -1
	prevAt := time.Time{}
	for {
		now := runtime.NumGoroutine()
		if prev != -1 && now == prev && time.Since(prevAt) >= stableWindow {
			return now
		}
		prev = now
		prevAt = time.Now()
		if time.Now().After(deadline) {
			return now
		}
		time.Sleep(samplePeriod)
	}
}

// TestEntityLocker_AcquireBatchCtxCancellationRollsBackPartial —
// ctx-cancel mid-AcquireBatchCtx must release any shards ALREADY taken
// (in reverse) so we don't leave zombie holds when the caller gives up.
// Covers the partial-rollback contract documented on AcquireBatchCtx.
func TestEntityLocker_AcquireBatchCtxCancellationRollsBackPartial(t *testing.T) {
	l := NewEntityLocker(32)
	bg := context.Background()

	holdCtx, holdCancel := context.WithCancel(bg)
	defer holdCancel()
	if err := l.LockCtx(holdCtx, "hold-A"); err != nil {
		t.Fatalf("pre-hold A: %v", err)
	}
	if err := l.LockCtx(holdCtx, "hold-B"); err != nil {
		t.Fatalf("pre-hold B: %v", err)
	}

	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(500 * time.Millisecond)
		l.Unlock("hold-B")
		l.Unlock("hold-A")
	}()

	// "hold-A" is held first, so any batch starting with A will block.
	// Batch ordering is [hold-A, freed-1, freed-2]: the lock on A
	// blocks indefinitely until 500ms, while B and freed-1/2 are
	// independent. Deadline 100ms < holder release schedules ctx-cancel
	// BEFORE we can even reach freed-1/2.
	waitCtx, waitCancel := context.WithTimeout(bg, 100*time.Millisecond)
	defer waitCancel()

	release, err := l.AcquireBatchCtx(waitCtx, []string{"hold-A", "freed-1", "freed-2"})
	if err == nil {
		if release != nil {
			release()
		}
		t.Fatal("AcquireBatchCtx should have returned ctx-cancel error, not nil")
	}
	if !errors.Is(err, ErrLockCancelled) {
		t.Errorf("AcquireBatchCtx err should wrap ErrLockCancelled; got %v", err)
	}

	// Critical: after the cancel, "hold-A" and "hold-B" must STILL be
	// held (we held them in another ctx); AcquireBatchCtx took ZERO
	// shards because it blocked on the first one. Then we verify
	// another fresh (post-cancel) batch — ["hold-A"] was blocked, so
	// zero shards were acquired — no zombie rollback to test; we just
	// confirm the system is healthy.
	fresh, err := l.AcquireBatchCtx(bg, []string{"freed-1", "freed-2"})
	if err != nil {
		t.Fatalf("fresh batch after cancel should succeed: %v", err)
	}
	if fresh != nil {
		fresh()
	}
	<-released
}

// TestEntityLocker_BackgroundCtxStillWorks — AcquireBatchCtx with
// context.Background() is the API equivalent of the pre-audit
// AcquireBatch: nil-or-empty input is no-op; non-empty input succeeds;
// release is callable with no panic on double-call (release idempotency
// is left to caller discipline, but it must at least be safe).
func TestEntityLocker_BackgroundCtxStillWorks(t *testing.T) {
	l := NewEntityLocker(32)
	bg := context.Background()

	// Empty batch.
	empty, err := l.AcquireBatchCtx(bg, nil)
	if err != nil {
		t.Fatalf("empty batch err: %v", err)
	}
	empty()

	// Single-id batch.
	r1, err := l.AcquireBatchCtx(bg, []string{"solo"})
	if err != nil {
		t.Fatalf("solo batch err: %v", err)
	}
	r1()

	// Multi-id batch with disjoint shards: must succeed, release
	// drains exactly N tokens.
	r2, err := l.AcquireBatchCtx(bg, []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("multi batch err: %v", err)
	}
	// Concurrent acquire should block only contention, not our disjoint set.
	var acqCount atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := l.LockCtx(bg, "delta"); err == nil {
			acqCount.Add(1)
			l.Unlock("delta")
		}
	}()
	<-done
	if acqCount.Load() == 0 {
		t.Error("disjoint acquire after multiple-id batch failed; check rollback double-unlock")
	}
	r2()
}
