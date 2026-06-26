package detectors

import (
	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
)

// CompositeDetector runs a fixed-order pipeline of ContradictionDetector
// stages and short-circuits on the first hit.
type CompositeDetector struct {
	detectors []contradiction.ContradictionDetector
}

// NewCompositeDetector returns a CompositeDetector wrapping the given
// detector stages in order.
func NewCompositeDetector(detectors ...contradiction.ContradictionDetector) *CompositeDetector {
	return &CompositeDetector{detectors: detectors}
}

// Detect runs the pipeline in order and returns the first hit's
// full DetectionResult verbatim. If no detector fires, returns the
// zero-value DetectionResult.
func (c *CompositeDetector) Detect(existing, incoming core.Entity) contradiction.DetectionResult {
	for _, d := range c.detectors {
		if d == nil {
			continue
		}
		if result := d.Detect(existing, incoming); result.Detected {
			return result
		}
	}
	return contradiction.DetectionResult{}
}
