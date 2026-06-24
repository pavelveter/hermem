// Package safego provides a panic-recovering wrapper around `go func()` for
// background goroutines that must NOT be able to crash the parent process.
//
// The parent os.Process dies when its last goroutine panics — recover() in
// one goroutine does NOT catch panics in another. Long-running auxiliaries
// (SIGHUP reload loops, batch processors, async flushers) should be wrapped
// in safego.Go so a panic inside them is logged + contained instead of
// killing the daemon.
//
// Usage:
//
//	safego.Go(rootCtx, "name-of-routine", func(ctx context.Context) {
//	    for range someInputChannel {
//	        process() // panics here are caught and logged
//	    }
//	})
//
// The fn is responsible for honouring ctx.Done() on its own iteration
// boundaries; safego does NOT cancel fn on its behalf — that's the fn's job
// since it owns the work.
package safego

import (
	"context"
	"log/slog"
	"runtime/debug"
)

// Go spawns fn in a goroutine with deferred panic recovery. The recovered
// value (panic + stack) is logged via slog.Error under the key "name" so an
// operator can correlate the crash back to the spawn site.
//
// Returns nothing — there is no way for a caller to await fn's completion
// or its panic-status. Use a dedicated group.Wait() on the fn when you need
// to coordinate shutdown.
func Go(ctx context.Context, name string, fn func(context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("safego: panic recovered",
					"name", name,
					"panic", r,
					"stack", string(debug.Stack()))
			}
		}()
		fn(ctx)
	}()
}
