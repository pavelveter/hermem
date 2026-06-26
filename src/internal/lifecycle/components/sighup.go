package components

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/pavelveter/hermem/src/internal/util/safego"
)

// SIGHUPHandler is a callback invoked on each SIGHUP signal.
// If the handler returns an error, it is logged but the loop continues.
type SIGHUPHandler func(ctx context.Context) error

// SIGHUPComponent listens for SIGHUP signals and invokes a handler.
type SIGHUPComponent struct {
	handler SIGHUPHandler
	cancel  context.CancelFunc
}

// NewSIGHUPComponent creates a Component that listens for SIGHUP and
// calls handler on each signal.
func NewSIGHUPComponent(handler SIGHUPHandler) *SIGHUPComponent {
	return &SIGHUPComponent{handler: handler}
}

// Start launches the SIGHUP listener in a background goroutine.
func (c *SIGHUPComponent) Start(ctx context.Context) error {
	loopCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)

	safego.Go(loopCtx, "sighup-reload", func(_ context.Context) {
		for range sighup {
			if err := c.handler(loopCtx); err != nil {
				slog.Error("SIGHUP handler", "err", err)
			}
		}
	})
	return nil
}

// Stop stops the SIGHUP listener by cancelling its context.
func (c *SIGHUPComponent) Stop(_ context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}
