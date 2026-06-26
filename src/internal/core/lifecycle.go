package core

import "context"

// Component is the unified lifecycle interface for long-lived
// background goroutines (HTTP server, GC loop, metrics worker,
// SIGHUP handler, etc.).
//
// Start launches the component. If Start returns an error, the
// component is considered failed and must not be used. The ctx
// passed to Start is the application-wide context — the component
// should respect its cancellation for graceful shutdown.
//
// Stop signals the component to shut down. The ctx carries a
// deadline for forced termination. Implementations must not
// return ctx.Err() when they finish cleanup — return nil on
// successful shutdown even if ctx expired.
type Component interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
