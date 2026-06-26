package lifecycle

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Manager orchestrates the lifecycle of registered core.Component
// instances. Components are started in registration order and stopped
// in reverse order, mirroring stack semantics.
//
// Usage:
//
//	mgr := lifecycle.NewManager()
//	mgr.Register(httpComp)
//	mgr.Register(gcComp)
//	if err := mgr.Start(ctx); err != nil { ... }
//	<-ctx.Done()
//	mgr.Stop(context.Background())
type Manager struct {
	mu         sync.Mutex
	components []core.Component
	started    []core.Component // subset that successfully Start'd
}

// NewManager constructs an empty Manager.
func NewManager() *Manager {
	return &Manager{}
}

// Register adds a component to the manager. Panics if called after
// Start — use the returned error from Start to decide whether to
// register more components.
func (m *Manager) Register(c core.Component) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.components = append(m.components, c)
}

// Start calls Start(ctx) on every registered component in order.
// If any Start fails, stops already-started components in reverse
// order and returns the first error. Components that were not yet
// started are skipped.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.components {
		if err := c.Start(ctx); err != nil {
			// Roll back: stop already-started in reverse.
			for i := len(m.started) - 1; i >= 0; i-- {
				if stopErr := m.started[i].Stop(context.Background()); stopErr != nil {
					slog.Error("lifecycle: rollback stop failed", "err", stopErr)
				}
			}
			return err
		}
		m.started = append(m.started, c)
	}
	return nil
}

// Stop calls Stop(ctx) on every started component in reverse order.
// Errors are collected, not short-circuited — every component gets
// a chance to stop.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for i := len(m.started) - 1; i >= 0; i-- {
		if err := m.started[i].Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
