package bench

import (
	"sort"

	"github.com/pavelveter/hermem/src/internal/metrics"
)

// knownLabelValues reads the label value set for a given histogram metric
// name from the Prometheus registry. It returns the distinct label values
// found in the registry's gathered MetricFamilies.
func knownLabelValues(m *metrics.Metrics, metricName, labelName string) []string {
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.GetMetric() {
			for _, lp := range metric.GetLabel() {
				if lp.GetName() == labelName {
					seen[lp.GetValue()] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// filterSentinel removes the "_init" sentinel from a label slice.
func filterSentinel(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l != "_init" {
			out = append(out, l)
		}
	}
	return out
}

// warmLabels ensures all non-sentinel labels are materialized in the
// given Metrics instance by emitting one zero-duration observation per
// label. After this call, knownLabelValues returns the full label set.
func warmLabels(m *metrics.Metrics) {
	for _, cat := range []string{"observation", "world", "task", "edge"} {
		m.ObserveIngestDuration(0, cat)
	}
	for _, mode := range []string{"search", "retrieve", "query", "response", "query_explain", "provenance"} {
		m.ObserveRetrievalDuration(0, mode)
	}
	for _, det := range []string{"lexical", "composite"} {
		m.ObserveContradictionDuration(0, det)
	}
	for _, strat := range []string{"llm_openai", "llm_ollama", "noop"} {
		m.ObserveRerankDuration(0, strat)
	}
}
