package bench

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/metrics"
)

func TestSynthesizeIngest_ReturnsCorrectCount(t *testing.T) {
	m := metrics.New()
	warmLabels(m)
	labels := filterSentinel(knownLabelValues(m, "hermem_ingest_duration_seconds", "category"))
	samples := SynthesizeIngest(m, 200, labels)
	if len(samples) != 200 {
		t.Fatalf("expected 200, got %d", len(samples))
	}
}

func TestSynthesizeRetrieval_ReturnsCorrectCount(t *testing.T) {
	m := metrics.New()
	warmLabels(m)
	labels := filterSentinel(knownLabelValues(m, "hermem_retrieval_duration_seconds", "mode"))
	samples := SynthesizeRetrieval(m, 200, labels)
	if len(samples) != 200 {
		t.Fatalf("expected 200, got %d", len(samples))
	}
}

func TestSynthesizeContradiction_ReturnsCorrectCount(t *testing.T) {
	m := metrics.New()
	warmLabels(m)
	labels := filterSentinel(knownLabelValues(m, "hermem_contradiction_duration_seconds", "detector"))
	samples := SynthesizeContradiction(m, 200, labels)
	if len(samples) != 200 {
		t.Fatalf("expected 200, got %d", len(samples))
	}
}

func TestSynthesizeRerank_ReturnsCorrectCount(t *testing.T) {
	m := metrics.New()
	warmLabels(m)
	labels := filterSentinel(knownLabelValues(m, "hermem_rerank_duration_seconds", "strategy"))
	samples := SynthesizeRerank(m, 200, labels)
	if len(samples) != 200 {
		t.Fatalf("expected 200, got %d", len(samples))
	}
}

func TestSynthesizeIngest_CyclesLabels(t *testing.T) {
	m := metrics.New()
	warmLabels(m)
	labels := filterSentinel(knownLabelValues(m, "hermem_ingest_duration_seconds", "category"))
	samples := SynthesizeIngest(m, 6, labels)
	// With 4 categories and 6 samples: labels[0], labels[1], labels[2], labels[3], labels[0], labels[1]
	for i, s := range samples {
		want := labels[i%len(labels)]
		if s.Label != want {
			t.Errorf("sample %d: label=%q, want %q", i, s.Label, want)
		}
	}
}

func TestPercentiles_EdgeCases(t *testing.T) {
	// nil slice
	p50, p95, p99 := Percentiles(nil)
	if p50 != 0 || p95 != 0 || p99 != 0 {
		t.Errorf("nil: expected all zeros, got %v %v %v", p50, p95, p99)
	}

	// Empty slice
	p50, p95, p99 = Percentiles([]float64{})
	if p50 != 0 || p95 != 0 || p99 != 0 {
		t.Errorf("empty: expected all zeros, got %v %v %v", p50, p95, p99)
	}
}
