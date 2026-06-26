package bench

import (
	"math"
	"sort"

	"github.com/pavelveter/hermem/src/internal/metrics"
)

// SampleResult holds one synthetic observation fed into a HistogramVec.
type SampleResult struct {
	Label string
	Value float64
}

// SynthesizeIngest feeds N observations into hIngest, cycling through
// the provided labels. Returns one SampleResult per observation.
func SynthesizeIngest(m *metrics.Metrics, n int, labels []string) []SampleResult {
	out := make([]SampleResult, n)
	for i := 0; i < n; i++ {
		lbl := labels[i%len(labels)]
		val := syntheticDuration(i)
		m.ObserveIngestDuration(val, lbl)
		out[i] = SampleResult{Label: lbl, Value: val}
	}
	return out
}

// SynthesizeRetrieval feeds N observations into hRetrieval.
func SynthesizeRetrieval(m *metrics.Metrics, n int, labels []string) []SampleResult {
	out := make([]SampleResult, n)
	for i := 0; i < n; i++ {
		lbl := labels[i%len(labels)]
		val := syntheticDuration(i)
		m.ObserveRetrievalDuration(val, lbl)
		out[i] = SampleResult{Label: lbl, Value: val}
	}
	return out
}

// SynthesizeContradiction feeds N observations into hContradiction.
func SynthesizeContradiction(m *metrics.Metrics, n int, labels []string) []SampleResult {
	out := make([]SampleResult, n)
	for i := 0; i < n; i++ {
		lbl := labels[i%len(labels)]
		val := syntheticDuration(i)
		m.ObserveContradictionDuration(val, lbl)
		out[i] = SampleResult{Label: lbl, Value: val}
	}
	return out
}

// SynthesizeRerank feeds N observations into hRerank.
func SynthesizeRerank(m *metrics.Metrics, n int, labels []string) []SampleResult {
	out := make([]SampleResult, n)
	for i := 0; i < n; i++ {
		lbl := labels[i%len(labels)]
		val := syntheticDuration(i)
		m.ObserveRerankDuration(val, lbl)
		out[i] = SampleResult{Label: lbl, Value: val}
	}
	return out
}

// syntheticDuration produces a deterministic but varied duration in seconds.
// Uses a sine-wave pattern so the distribution covers multiple buckets.
func syntheticDuration(i int) float64 {
	base := 0.5 + 4.5*math.Sin(float64(i)*0.017)
	jitter := float64(i%7) * 0.003
	return base + jitter
}

// Percentiles computes p50, p95, p99 from a sorted slice of durations.
// Input must be sorted ascending. Returns 0 for empty slices.
func Percentiles(sorted []float64) (p50, p95, p99 float64) {
	n := len(sorted)
	if n == 0 {
		return 0, 0, 0
	}
	p50 = sorted[n*50/100]
	p95 = sorted[n*95/100]
	p99 = sorted[n*99/100]
	return
}

// LabelCounts returns a map of label → observation count.
func LabelCounts(samples []SampleResult) map[string]int {
	m := map[string]int{}
	for _, s := range samples {
		m[s.Label]++
	}
	return m
}

// SortedValues extracts and sorts the Value field from samples.
func SortedValues(samples []SampleResult) []float64 {
	out := make([]float64, len(samples))
	for i, s := range samples {
		out[i] = s.Value
	}
	sort.Float64s(out)
	return out
}
