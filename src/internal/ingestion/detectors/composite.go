package detectors

import (
	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
)

// CompositeDetector runs a fixed-order pipeline of ContradictionDetector
// stages. The first detector (lexical) acts as a fast filter:
//   - If it returns Detected=true → run remaining detectors to verify
//   - If it returns Inconclusive=true → run remaining detectors
//   - If it returns Detected=false (no Inconclusive) → skip (definitive no)
type CompositeDetector struct {
	detectors []contradiction.ContradictionDetector
}

// NewCompositeDetector returns a CompositeDetector wrapping the given
// detector stages in order.
func NewCompositeDetector(detectors ...contradiction.ContradictionDetector) *CompositeDetector {
	return &CompositeDetector{detectors: detectors}
}

// Detect runs the pipeline. The first detector is a fast filter.
// If it fires (Detected) or is uncertain (Inconclusive), remaining
// detectors run to verify. The last confirming result wins.
// If the first detector definitively says no (not Detected, not
// Inconclusive), the pipeline short-circuits.
func (c *CompositeDetector) Detect(existing, incoming core.Entity) contradiction.DetectionResult {
	if len(c.detectors) == 0 {
		return contradiction.DetectionResult{}
	}

	first := c.detectors[0]
	if first == nil {
		return contradiction.DetectionResult{}
	}
	firstResult := first.Detect(existing, incoming)

	// Definitive no — short-circuit.
	if !firstResult.Detected && !firstResult.Inconclusive {
		return contradiction.DetectionResult{}
	}

	// Single detector — propagate its result.
	if len(c.detectors) == 1 {
		return firstResult
	}

	// Verify with remaining detectors. Return the last confirming result.
	var last contradiction.DetectionResult
	for _, d := range c.detectors[1:] {
		if d == nil {
			continue
		}
		if result := d.Detect(existing, incoming); result.Detected {
			last = result
		}
	}
	if last.Detected {
		return last
	}
	// No heavier detector confirmed.
	return contradiction.DetectionResult{}
}
