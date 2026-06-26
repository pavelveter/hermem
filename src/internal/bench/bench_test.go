package bench

import (
	"sort"
	"testing"

	"github.com/pavelveter/hermem/src/internal/metrics"
)

func TestSynthesizeIngest(t *testing.T) {
	m := metrics.New()
	warmLabels(m)
	labels := filterSentinel(knownLabelValues(m, "hermem_ingest_duration_seconds", "category"))
	if len(labels) == 0 {
		t.Fatal("no labels found after warmLabels")
	}

	samples := SynthesizeIngest(m, 100, labels)
	if len(samples) != 100 {
		t.Fatalf("expected 100 samples, got %d", len(samples))
	}
	// Verify all labels are cycled through.
	seen := map[string]bool{}
	for _, s := range samples {
		seen[s.Label] = true
		if s.Value <= 0 {
			t.Errorf("sample value must be > 0, got %v", s.Value)
		}
	}
	for _, lbl := range labels {
		if !seen[lbl] {
			t.Errorf("label %q not observed", lbl)
		}
	}
}

func TestPercentiles(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		p50  float64
		p95  float64
		p99  float64
	}{
		{"empty", nil, 0, 0, 0},
		{"single", []float64{1.0}, 1.0, 1.0, 1.0},
		{"sorted_10", []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}, 0.6, 1.0, 1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sorted := make([]float64, len(c.in))
			copy(sorted, c.in)
			sort.Float64s(sorted)
			p50, p95, p99 := Percentiles(sorted)
			if p50 != c.p50 {
				t.Errorf("p50 = %v, want %v", p50, c.p50)
			}
			if p95 != c.p95 {
				t.Errorf("p95 = %v, want %v", p95, c.p95)
			}
			if p99 != c.p99 {
				t.Errorf("p99 = %v, want %v", p99, c.p99)
			}
		})
	}
}

func TestLabelCounts(t *testing.T) {
	samples := []SampleResult{
		{Label: "a", Value: 1},
		{Label: "b", Value: 2},
		{Label: "a", Value: 3},
	}
	counts := LabelCounts(samples)
	if counts["a"] != 2 {
		t.Errorf("a count = %d, want 2", counts["a"])
	}
	if counts["b"] != 1 {
		t.Errorf("b count = %d, want 1", counts["b"])
	}
}

func TestSortedValues(t *testing.T) {
	samples := []SampleResult{
		{Label: "a", Value: 3},
		{Label: "b", Value: 1},
		{Label: "c", Value: 2},
	}
	vals := SortedValues(samples)
	if len(vals) != 3 {
		t.Fatalf("expected 3 values, got %d", len(vals))
	}
	if vals[0] != 1 || vals[1] != 2 || vals[2] != 3 {
		t.Errorf("expected [1,2,3], got %v", vals)
	}
}

func TestFilterSentinel(t *testing.T) {
	in := []string{"_init", "a", "b"}
	out := filterSentinel(in)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	if out[0] != "a" || out[1] != "b" {
		t.Errorf("expected [a,b], got %v", out)
	}
}

func TestSynthesizeAllVectors(t *testing.T) {
	m := metrics.New()
	warmLabels(m)

	n := 50
	cats := filterSentinel(knownLabelValues(m, "hermem_ingest_duration_seconds", "category"))
	modes := filterSentinel(knownLabelValues(m, "hermem_retrieval_duration_seconds", "mode"))
	dets := filterSentinel(knownLabelValues(m, "hermem_contradiction_duration_seconds", "detector"))
	strats := filterSentinel(knownLabelValues(m, "hermem_rerank_duration_seconds", "strategy"))

	if len(cats) == 0 {
		t.Fatal("no categories")
	}
	if len(modes) == 0 {
		t.Fatal("no modes")
	}
	if len(dets) == 0 {
		t.Fatal("no detectors")
	}
	if len(strats) == 0 {
		t.Fatal("no strategies")
	}

	s1 := SynthesizeIngest(m, n, cats)
	if len(s1) != n {
		t.Errorf("ingest: got %d, want %d", len(s1), n)
	}
	s2 := SynthesizeRetrieval(m, n, modes)
	if len(s2) != n {
		t.Errorf("retrieval: got %d, want %d", len(s2), n)
	}
	s3 := SynthesizeContradiction(m, n, dets)
	if len(s3) != n {
		t.Errorf("contradiction: got %d, want %d", len(s3), n)
	}
	s4 := SynthesizeRerank(m, n, strats)
	if len(s4) != n {
		t.Errorf("rerank: got %d, want %d", len(s4), n)
	}
}
