package detectors

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
)

type stubDetector struct {
	detected    bool
	inconclusive bool
	reason      string
	confidence  float32
	calls       int
}

func (s *stubDetector) Detect(_, _ core.Entity) contradiction.DetectionResult {
	s.calls++
	return contradiction.DetectionResult{
		Detected:    s.detected,
		Inconclusive: s.inconclusive,
		Reason:      s.reason,
		Confidence:  s.confidence,
	}
}

func TestCompositeDetector(t *testing.T) {
	t.Run("empty_pipeline_returns_miss", func(t *testing.T) {
		c := NewCompositeDetector()
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected || result.Reason != "" || result.Confidence != 0 {
			t.Fatalf("empty pipeline must return zero-value DetectionResult; got %+v", result)
		}
	})

	t.Run("single_definitive_no_returns_miss", func(t *testing.T) {
		miss := &stubDetector{detected: false, inconclusive: false}
		c := NewCompositeDetector(miss)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected {
			t.Fatalf("single definitive-no must return miss; got %+v", result)
		}
		if miss.calls != 1 {
			t.Fatalf("want called once; got %d", miss.calls)
		}
	})

	t.Run("single_inconclusive_returns_inconclusive", func(t *testing.T) {
		uncertain := &stubDetector{inconclusive: true}
		c := NewCompositeDetector(uncertain)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected {
			t.Fatalf("single inconclusive with no remaining should return inconclusive; got %+v", result)
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

	t.Run("inconclusive_triggers_verification", func(t *testing.T) {
		uncertain := &stubDetector{inconclusive: true}
		verifier := &stubDetector{detected: true, reason: "verified", confidence: 0.8}
		c := NewCompositeDetector(uncertain, verifier)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !result.Detected || result.Reason != "verified" {
			t.Fatalf("want verifier's result; got %+v", result)
		}
	})

	t.Run("inconclusive_no_confirmation_returns_miss", func(t *testing.T) {
		uncertain := &stubDetector{inconclusive: true}
		verifier := &stubDetector{detected: false}
		c := NewCompositeDetector(uncertain, verifier)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected {
			t.Fatalf("want miss (no confirmation); got %+v", result)
		}
	})

	t.Run("first_fires_second_verifies", func(t *testing.T) {
		first := &stubDetector{detected: true, reason: "stub:first", confidence: 0.9}
		second := &stubDetector{detected: true, reason: "stub:second", confidence: 0.5}
		c := NewCompositeDetector(first, second)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !result.Detected || result.Reason != "stub:second" || result.Confidence != 0.5 {
			t.Fatalf("want second's result (heavier verifies); got %+v", result)
		}
	})

	t.Run("first_fires_second_rejects_returns_miss", func(t *testing.T) {
		first := &stubDetector{detected: true, reason: "stub:first", confidence: 0.9}
		second := &stubDetector{detected: false}
		c := NewCompositeDetector(first, second)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected {
			t.Fatalf("want miss (second rejected); got %+v", result)
		}
	})

	t.Run("definitive_no_skips_remaining", func(t *testing.T) {
		first := &stubDetector{detected: false, inconclusive: false}
		second := &stubDetector{detected: true, reason: "stub:second", confidence: 0.6}
		c := NewCompositeDetector(first, second)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected {
			t.Fatalf("want miss (first definitively said no); got %+v", result)
		}
		if first.calls != 1 || second.calls != 0 {
			t.Fatalf("want first=1 second=0 (skipped); got first=%d second=%d", first.calls, second.calls)
		}
	})

	t.Run("nil_first_returns_miss", func(t *testing.T) {
		hit := &stubDetector{detected: true, reason: "stub:after_nil", confidence: 1.0}
		c := NewCompositeDetector(nil, hit)
		result := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if result.Detected {
			t.Fatalf("want miss (nil first); got %+v", result)
		}
	})
}
