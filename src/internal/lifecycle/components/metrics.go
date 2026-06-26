package components

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/metrics"
)

// MetricsComponent wraps metrics.AsyncMetricsWorker as a lifecycle.Component.
type MetricsComponent struct {
	worker *metrics.AsyncMetricsWorker
}

// NewMetricsComponent creates a Component that manages the metrics worker.
func NewMetricsComponent(worker *metrics.AsyncMetricsWorker) *MetricsComponent {
	return &MetricsComponent{worker: worker}
}

// Start launches the metrics worker background loop.
func (c *MetricsComponent) Start(_ context.Context) error {
	c.worker.Start()
	return nil
}

// Stop drains the metrics worker.
func (c *MetricsComponent) Stop(_ context.Context) error {
	c.worker.Stop()
	return nil
}
