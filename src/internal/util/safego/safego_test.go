package safego

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestGo_PanicIsRecovered — a panic inside the spawned fn must NOT
// propagate to the parent process. We register a deferred panic that
// would crash the test if any safego.Go call leaked a panic; if the
// test returns cleanly the recover() worked.
func TestGo_PanicIsRecovered(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("safego failed to contain panic: leaked %v", r)
		}
	}()
	Go(context.Background(), "panic-source", func(_ context.Context) {
		panic("kaboom")
	})
	// Give the goroutine a moment to actually run + recover.
	time.Sleep(50 * time.Millisecond)
}

// TestGo_NormalFnRuns deterministically — a fn that just signals a flag
// must run before the parent observes the flag (after a generous wait).
func TestGo_NormalFnRuns(t *testing.T) {
	var ran atomic.Bool
	Go(context.Background(), "normal", func(_ context.Context) {
		ran.Store(true)
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ran.Load() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("safego.Go did not run fn within 1s")
}

// TestGo_PassesCtx — fn receives a context. We cancel it before fn runs
// and assert fn observes ctx.Err(). Documents the contract that fn
// owns ctx cancellation, so a future regression there is caught.
func TestGo_PassesCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	var seen error
	var done atomic.Bool
	Go(ctx, "ctx-probe", func(c context.Context) {
		seen = c.Err()
		done.Store(true)
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if done.Load() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !done.Load() {
		t.Fatal("fn did not run within 1s")
	}
	if seen != context.Canceled {
		t.Fatalf("ctx.Err(): want Canceled, got %v", seen)
	}
}

// TestGo_DistinctNames logged for distinct panic sources — the contract
// is that the spawn-site's name is propagated to the slog.Error, not
// silently dropped. We just trigger two panics from different names
// to confirm both finish without leaking.
func TestGo_DistinctNames(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("leaked panic: %v", r)
		}
	}()
	Go(context.Background(), "src-a", func(_ context.Context) { panic("a") })
	Go(context.Background(), "src-b", func(_ context.Context) { panic("b") })
	time.Sleep(50 * time.Millisecond)
}
