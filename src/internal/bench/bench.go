package bench

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/metrics"
)

// VecReport holds the bench summary for one HistogramVec.
type VecReport struct {
	Label       string            `json:"label"`
	Histogram   string            `json:"histogram"`
	Count       int               `json:"count"`
	P50         float64           `json:"p50"`
	P95         float64           `json:"p95"`
	P99         float64           `json:"p99"`
	LabelCounts map[string]int    `json:"label_counts"`
	Buckets     map[string]uint64 `json:"buckets"`
}

// BenchReport is the top-level JSON envelope.
type BenchReport struct {
	Iterations int         `json:"iterations"`
	Series     []VecReport `json:"series"`
}

// NewCmd returns the bench cobra subcommand.
func NewCmd(env *clienv.Env) *cobra.Command {
	var iterations int
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:               "bench",
		Short:             "Synthesize N observations into each duration histogram and report latency percentiles",
		Args:              cobra.NoArgs,
		PersistentPreRunE: noopPreRun,
		SilenceErrors:     true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runBench(env, iterations, jsonOutput)
		},
	}
	cmd.Flags().IntVar(&iterations, "iterations", 5000, "number of observations per histogram")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output machine-readable JSON instead of human text")
	return cmd
}

// noopPreRun skips DB init — bench only touches the metrics layer.
var noopPreRun = func(_ *cobra.Command, _ []string) error { return nil }

func runBench(env *clienv.Env, iterations int, jsonOutput bool) error {
	if iterations <= 0 {
		iterations = 5000
	}

	// Use env.Metrics if available, otherwise create a fresh one.
	var m *metrics.Metrics
	if env != nil && env.Metrics != nil {
		m = env.Metrics
	} else {
		m = metrics.New()
	}

	// Warm all non-sentinel labels so they appear in the registry.
	warmLabels(m)

	// Read the label sets at runtime from the Prometheus registry.
	cats := filterSentinel(knownLabelValues(m, "hermem_ingest_duration_seconds", "category"))
	modes := filterSentinel(knownLabelValues(m, "hermem_retrieval_duration_seconds", "mode"))
	dets := filterSentinel(knownLabelValues(m, "hermem_contradiction_duration_seconds", "detector"))
	strats := filterSentinel(knownLabelValues(m, "hermem_rerank_duration_seconds", "strategy"))

	// Phase 1: synthesize observations.
	ingestSamples := SynthesizeIngest(m, iterations, cats)
	retrievalSamples := SynthesizeRetrieval(m, iterations, modes)
	contradictionSamples := SynthesizeContradiction(m, iterations, dets)
	rerankSamples := SynthesizeRerank(m, iterations, strats)

	// Phase 2: read back from Prometheus registry and build reports.
	reports := []VecReport{
		buildReport(m, "hermem_ingest_duration_seconds", "category", ingestSamples),
		buildReport(m, "hermem_retrieval_duration_seconds", "mode", retrievalSamples),
		buildReport(m, "hermem_contradiction_duration_seconds", "detector", contradictionSamples),
		buildReport(m, "hermem_rerank_duration_seconds", "strategy", rerankSamples),
	}

	if jsonOutput {
		return writeJSON(env, BenchReport{Iterations: iterations, Series: reports})
	}
	writeText(iterations, reports)
	return nil
}

// buildReport reads histogram data from the Prometheus registry for the
// given metric name and combines it with the synthetic samples to produce
// a full VecReport.
func buildReport(m *metrics.Metrics, metricName, labelCol string, samples []SampleResult) VecReport {
	sorted := SortedValues(samples)
	p50, p95, p99 := Percentiles(sorted)
	lc := LabelCounts(samples)

	// Read bucket distribution from the Prometheus registry.
	buckets := readBuckets(m, metricName)

	return VecReport{
		Label:       labelCol,
		Histogram:   metricName,
		Count:       len(samples),
		P50:         p50,
		P95:         p95,
		P99:         p99,
		LabelCounts: lc,
		Buckets:     buckets,
	}
}

// readBuckets gathers from the Prometheus registry and extracts the
// cumulative bucket counts for the named histogram metric.
func readBuckets(m *metrics.Metrics, metricName string) map[string]uint64 {
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		slog.Warn("bench: failed to gather prometheus metrics", "err", err)
		return nil
	}
	out := map[string]uint64{}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, metric := range mf.GetMetric() {
			h := metric.GetHistogram()
			if h == nil {
				continue
			}
			for _, b := range h.GetBucket() {
				upper := formatBucketBound(b.GetUpperBound())
				out[upper] = b.GetCumulativeCount()
			}
		}
	}
	return out
}

// formatBucketBound formats a Prometheus histogram bucket upper bound.
// +Inf is rendered as "+Inf"; finite bounds are rendered with 3 decimals.
func formatBucketBound(bound float64) string {
	if bound > 1e18 {
		return "+Inf"
	}
	return fmt.Sprintf("%.3f", bound)
}

func writeText(iterations int, reports []VecReport) {
	slog.Info("bench complete",
		"iterations", iterations,
	)
	for _, r := range reports {
		slog.Info("series",
			"label", r.Label,
			"histogram", r.Histogram,
			"count", r.Count,
			"p50", fmt.Sprintf("%.3f", r.P50),
			"p95", fmt.Sprintf("%.3f", r.P95),
			"p99", fmt.Sprintf("%.3f", r.P99),
		)
		// Log per-label counts.
		var parts []string
		for lbl, cnt := range r.LabelCounts {
			parts = append(parts, fmt.Sprintf("%s=%d", lbl, cnt))
		}
		slog.Info("label_counts", "series", r.Label, "counts", strings.Join(parts, ", "))

		// Log bucket distribution.
		var bparts []string
		for bucket, count := range r.Buckets {
			bparts = append(bparts, fmt.Sprintf("<=%s:%d", bucket, count))
		}
		slog.Info("buckets", "series", r.Label, "distribution", strings.Join(bparts, ", "))
	}
}

func writeJSON(_ *clienv.Env, report BenchReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
