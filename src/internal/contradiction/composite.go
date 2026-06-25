package contradiction

import "github.com/pavelveter/hermem/src/internal/core"

// CompositeDetector runs a fixed-order pipeline of ContradictionDetector
// stages and short-circuits on the first hit.
//
// The pipeline semantics are deliberately trivial: each detector is
// invoked with the same (existing, incoming) pair, and the first one
// to report Detected=true wins. This lets a cheap lexical pass run
// before a more expensive semantic pass (future commit) without the
// upstream caller having to know about either.
//
// The detectors slice is captured by reference (it's a Go slice
// header), so callers who need to mutate the pipeline after
// construction must do so before passing the slice in — there is no
// SetDetector / AddDetector helper on purpose.
type CompositeDetector struct {
	detectors []ContradictionDetector
}

// NewCompositeDetector returns a CompositeDetector wrapping the given
// detector stages in order. An empty pipeline is allowed and resolves
// every Detect call to (false, "") — see the Detect godoc for the
// defensive rationale.
func NewCompositeDetector(detectors ...ContradictionDetector) *CompositeDetector {
	return &CompositeDetector{detectors: detectors}
}

// Detect runs the pipeline in order and returns the first hit's
// (Detected=true, reason). If no detector fires — including the
// empty-pipeline case — returns (false, "") so callers never have to
// guard against a misconfigured pipeline panicking on len()==0.
func (c *CompositeDetector) Detect(existing, incoming core.Entity) (bool, string) {
	for _, d := range c.detectors {
		if d == nil {
			continue
		}
		if detected, reason := d.Detect(existing, incoming); detected {
			return true, reason
		}
	}
	return false, ""
}
