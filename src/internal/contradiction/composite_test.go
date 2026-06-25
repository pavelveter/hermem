package contradiction

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// stubDetector is a ContradictionDetector that returns a canned
// (detected, reason) pair. Use it to drive CompositeDetector's
// short-circuit logic without coupling tests to the lexical
// heuristic.
type stubDetector struct {
	detected bool
	reason   string
	calls    int
}

func (s *stubDetector) Detect(_, _ core.Entity) (bool, string) {
	s.calls++
	return s.detected, s.reason
}

// TestCompositeDetector locks the pipeline semantics: ordered,
// short-circuiting, defensive on empty input and nil entries.
//
// The cases below map 1:1 to the plan's table:
//
//   - Empty pipeline → (false, "") — defensive, no panic.
//   - Single non-firing detector → (false, "") — propagated miss.
//   - Single firing detector → propagates (true, reason) — first hit wins.
//   - Two detectors, second fires → second's reason.
//   - Two detectors, first fires → first wins (order matters; second
//     must NOT be called — proves short-circuit, not just propagation).
//   - Three-detector chain, middle fires → middle's result, third
//     must NOT be called (short-circuit holds across chain).
func TestCompositeDetector(t *testing.T) {
	t.Run("empty_pipeline_returns_miss", func(t *testing.T) {
		c := NewCompositeDetector()
		got, reason := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if got || reason != "" {
			t.Fatalf("empty pipeline must return (false, \"\"); got (%v, %q)", got, reason)
		}
	})

	t.Run("single_non_firing_returns_miss", func(t *testing.T) {
		miss := &stubDetector{detected: false}
		c := NewCompositeDetector(miss)
		got, reason := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if got || reason != "" {
			t.Fatalf("single non-firing detector must return (false, \"\"); got (%v, %q)", got, reason)
		}
		if miss.calls != 1 {
			t.Fatalf("want miss detector called exactly once; got %d", miss.calls)
		}
	})

	t.Run("single_firing_propagates_reason", func(t *testing.T) {
		hit := &stubDetector{detected: true, reason: "stub:hit"}
		c := NewCompositeDetector(hit)
		got, reason := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !got || reason != "stub:hit" {
			t.Fatalf("want (true, %q); got (%v, %q)", "stub:hit", got, reason)
		}
	})

	t.Run("second_fires_when_first_misses", func(t *testing.T) {
		miss := &stubDetector{detected: false}
		hit := &stubDetector{detected: true, reason: "stub:second"}
		c := NewCompositeDetector(miss, hit)
		got, reason := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !got || reason != "stub:second" {
			t.Fatalf("want (true, %q); got (%v, %q)", "stub:second", got, reason)
		}
		if miss.calls != 1 || hit.calls != 1 {
			t.Fatalf("want each detector called exactly once; got miss=%d hit=%d", miss.calls, hit.calls)
		}
	})

	t.Run("first_fires_second_not_called", func(t *testing.T) {
		first := &stubDetector{detected: true, reason: "stub:first"}
		second := &stubDetector{detected: true, reason: "stub:second"}
		c := NewCompositeDetector(first, second)
		got, reason := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !got || reason != "stub:first" {
			t.Fatalf("want (true, %q); got (%v, %q)", "stub:first", got, reason)
		}
		if second.calls != 0 {
			t.Fatalf("want second NOT called (short-circuit); got calls=%d", second.calls)
		}
		if first.calls != 1 {
			t.Fatalf("want first called exactly once; got %d", first.calls)
		}
	})

	t.Run("middle_fires_third_not_called", func(t *testing.T) {
		first := &stubDetector{detected: false}
		middle := &stubDetector{detected: true, reason: "stub:middle"}
		third := &stubDetector{detected: true, reason: "stub:third"}
		c := NewCompositeDetector(first, middle, third)
		got, reason := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !got || reason != "stub:middle" {
			t.Fatalf("want (true, %q); got (%v, %q)", "stub:middle", got, reason)
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
		hit := &stubDetector{detected: true, reason: "stub:after_nil"}
		c := NewCompositeDetector(nil, hit)
		got, reason := c.Detect(core.Entity{Content: "a"}, core.Entity{Content: "b"})
		if !got || reason != "stub:after_nil" {
			t.Fatalf("want (true, %q); got (%v, %q)", "stub:after_nil", got, reason)
		}
	})
}
