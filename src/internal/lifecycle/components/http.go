package components

import (
	"context"
	"log/slog"
	"net/http"
)

// HTTPComponent wraps an *http.Server as a lifecycle.Component.
type HTTPComponent struct {
	srv *http.Server
}

// NewHTTPComponent creates a Component that starts the HTTP server
// in a goroutine and shuts it down via Shutdown on Stop.
func NewHTTPComponent(srv *http.Server) *HTTPComponent {
	return &HTTPComponent{srv: srv}
}

// Start launches the HTTP listener in a background goroutine.
// Returns immediately — server errors are logged, not surfaced to Start.
func (c *HTTPComponent) Start(_ context.Context) error {
	go func() {
		if err := c.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "err", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the HTTP server. The ctx carries a
// deadline for forced termination.
func (c *HTTPComponent) Stop(ctx context.Context) error {
	return c.srv.Shutdown(ctx)
}
