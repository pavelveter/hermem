package components

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retention"
)

// GCComponent wraps retention.Service as a lifecycle.Component.
// The GC loop runs until the context is cancelled.
type GCComponent struct {
	svc    *retention.Service
	policy core.RetentionPolicy
}

// NewGCComponent creates a Component that runs the retention sweep loop.
func NewGCComponent(svc *retention.Service, policy core.RetentionPolicy) *GCComponent {
	return &GCComponent{svc: svc, policy: policy}
}

// Start launches the GC loop in a background goroutine. The goroutine
// exits when ctx is cancelled.
func (c *GCComponent) Start(ctx context.Context) error {
	go c.svc.Run(ctx, c.policy)
	return nil
}

// Stop is a no-op — the GC loop exits when its context is cancelled,
// which happens before Stop is called.
func (c *GCComponent) Stop(_ context.Context) error {
	return nil
}
