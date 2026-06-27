package components

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retention"
	"github.com/pavelveter/hermem/src/internal/util/safego"
)

// GCComponent wraps retention.Service as a lifecycle.Component.
// The GC loop runs until the context is cancelled.
//
// ZOMBIE PROTECTION CONTRACT (Audit Part 6 #1): Start MUST wrap the
// goroutine in safego.Go so a panic inside the retention tick loop is
// LOGGED and CONTAINED rather than killing the parent process. Prior
// to the §6 closure a bare `go c.svc.Run(...)` could be torn down by
// the runtime's panic propagation, leaving the GC service silently
// dead while the rest of the daemon continued to serve /health 200s
// — exactly the "zombie application" failure mode the audit names.
//
// Mirror this contract in any future Component that spawns a long-
// lived worker: bare `go fn(...)` for a worker MUST NOT appear here.
type GCComponent struct {
	svc    *retention.Service
	policy core.RetentionPolicy
}

// NewGCComponent creates a Component that runs the retention sweep loop.
func NewGCComponent(svc *retention.Service, policy core.RetentionPolicy) *GCComponent {
	return &GCComponent{svc: svc, policy: policy}
}

// Start launches the GC loop in a background goroutine. The goroutine
// exits when ctx is cancelled. The goroutine is wrapped in safego.Go
// so a panic from inside the retention tick loop is logged with the
// stack trace and contained — the daemon survives as a (degraded) state
// until an operator restarts.
func (c *GCComponent) Start(ctx context.Context) error {
	safego.Go(ctx, "gc-sweep", func(ctx context.Context) {
		c.svc.Run(ctx, c.policy)
	})
	return nil
}

// Stop is a no-op — the GC loop exits when its context is cancelled,
// which happens before Stop is called.
func (c *GCComponent) Stop(_ context.Context) error {
	return nil
}
