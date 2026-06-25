package contradiction

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// stubDetector is a ContradictionDetector that returns a canned
// DetectionResult. Use it to drive CompositeDetector's
// short-circuit logic without coupling tests to the lexical
// heuristic.
type stubDetector struct {
	detected   bool
	reason     string
	confidence float32
	calls      int
}

func (s *stubDetector) Detect(_, _ core.Entity) DetectionResult {
	s.calls++
	return DetectionResult{Detected: s.detected, Reason: s.reason, Confidence: s.confidence}
}

// TestCompositeDetector locks the pipeline semantics: ordered,
// short-circuiting, defensive on empty input and nil entries, and
// propagating the first hit's full DetectionResult verbatim
// (including Confidence).
//
// The cases below map 1:1 to the plan's table:
//
//   - Empty pipeline → zero-value DetectionResult — defensive, no panic.
//   - Single non-firing detector → zero-value DetectionResult — propagated miss.
//   - Single firing detector → propagates the detector's full result.
//   - Two detectors, second fires → second's full result (confidence included).
//   - Two detectors, first fires → first wins (order matters; second
//     must NOT be called — proves short-circuit, not just propagation).
//   - Three-detector chain, middle fires → middle's result, third
//     must NOT be called (short-circuit holds across chain).
func TestCompositeDetector(t *testing.T) {
	t.Run("empty_pipeline_returns_miss", func(t *testing.T) {
		c := NewCompositeDetector()
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected || result.Reason != "" || result.Confidence != 0 {
			t.Fatalf("empty pipeline must return zero-value DetectionResult; got %+v", result)
		}
	})

	t.Run("single_non_firing_returns_miss", func(t *testing.T) {
		miss := &stubDetector{}
		c := NewCompositeDetector(miss)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected || result.Reason != "" || result.Confidence != 0 {
			t.Fatalf("single non-firing detector must return zero-value DetectionResult; got %+v", result)
		}
		if miss.calls != 1 {
			t.Fatalf("want miss detector called exactly once; got %d", miss.calls)
		}
	})

	t.Run("single_firing_propagates_result", func(t *testing.T) {
		hit := &stubDetector{detected: true, reason: "stub:hit", confidence: 0.42}
		c := NewCompositeDetector(hit)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !result.Detected || result.Reason != "stub:hit" || result.Confidence != 0.42 {
			t.Fatalf("want DetectionResult{true, \"stub:hit\", 0.42}; got %+v", result)
		}
	})

	t.Run("second_fires_when_first_misses", func(t *testing.T) {
		miss := &stubDetector{}
		hit := &stubDetector{detected: true, reason: "stub:second", confidence: 0.6}
		c := NewCompositeDetector(miss, hit)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !result.Detected || result.Reason != "stub:second" || result.Confidence != 0.6 {
			t.Fatalf("want DetectionResult{true, \"stub:second\", 0.6}; got %+v", result)
		}
		if miss.calls != 1 || hit.calls != 1 {
			t.Fatalf("want each detector called exactly once; got miss=%d hit=%d", miss.calls, hit.calls)
		}
	})

	t.Run("first_fires_second_not_called", func(t *testing.T) {
		first := &stubDetector{detected: true, reason: "stub:first", confidence: 0.9}
		second := &stubDetector{detected: true, reason: "stub:second", confidence: 0.5}
		c := NewCompositeDetector(first, second)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !result.Detected || result.Reason != "stub:first" || result.Confidence != 0.9 {
			t.Fatalf("want first's DetectionResult propagated; got %+v", result)
		}
		if second.calls != 0 {
			t.Fatalf("want second NOT called (short-circuit); got calls=%d", second.calls)
		}
		if first.calls != 1 {
			t.Fatalf("want first called exactly once; got %d", first.calls)
		}
	})

	t.Run("middle_fires_third_not_called", func(t *testing.T) {
		first := &stubDetector{}
		middle := &stubDetector{detected: true, reason: "stub:middle", confidence: 0.7}
		third := &stubDetector{detected: true, reason: "stub:third", confidence: 0.3}
		c := NewCompositeDetector(first, middle, third)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !result.Detected || result.Reason != "stub:middle" || result.Confidence != 0.7 {
			t.Fatalf("want middle's DetectionResult propagated; got %+v", result)
		}
		if third.calls != 0 {
			t.Fatalf("want third NOT called (short-circuit); got calls=%d", third.calls)
		}
		if first.calls != 1 || middle.calls != 1 {
			t.Fatalf("want first=1 middle=1; got first=%d middle=%d", first.calls, middle.calls)
		}
	})

	t.Run("nil_entry_is_skipped", func(t *testing.T) {
		// Defensive: a nil entry in the pipeline must NOT panic; it
		// is skipped and the next detector is consulted. This
		// prevents a misconfigured pipeline (e.g. an unset
		// detector slot) from blowing up the whole ingest path.
		hit := &stubDetector{detected: true, reason: "stub:after_nil", confidence: 1.0}
		c := NewCompositeDetector(nil, hit)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !result.Detected || result.Reason != "stub:after_nil" || result.Confidence != 1.0 {
			t.Fatalf("want DetectionResult after nil skip; got %+v", result)
		}
	})
}
