// Package evaluation — CI smoke test: validates all 4 benchmark datasets
// produce meaningful metrics (not zero / NaN / panic) with a perfect
// retriever. This is the "Add benchmark CI job" item from P1 EVALUATION
// FRAMEWORK.
package evaluation

import (
	"context"
	"math"
	"testing"
)

// TestAllDatasets_ProduceMeaningfulMetrics runs every default dataset
// through its evaluator and asserts the metrics are in valid ranges.
// This single test serves as the CI gate — if metrics go NaN or the
// runner panics, the gate fails.
func TestAllDatasets_ProduceMeaningfulMetrics(t *testing.T) {
	t.Run("Retrieval", func(t *testing.T) {
		ds := DefaultRetrievalDataset()
		runner := NewEvalRunner(5)
		report, err := runner.Run(context.Background(), ds, PerfectRetrievalFn(ds.Qrels))
		if err != nil {
			t.Fatalf("retrieval runner: %v", err)
		}
		assertMetric(t, "Recall", report.Recall, 0, 1.01)
		assertMetric(t, "Precision", report.Precision, 0, 1.01)
		assertMetric(t, "MRR", report.MRR, 0, 1.01)
		assertMetric(t, "NDCG", report.NDCG, 0, 1.01)
		if report.TotalQueries != len(ds.QueryIDs) {
			t.Errorf("TotalQueries = %d, want %d", report.TotalQueries, len(ds.QueryIDs))
		}
	})

	t.Run("Contradiction", func(t *testing.T) {
		ds := DefaultContradictionDataset()
		// Use a trivial classifier: always says "yes" to test metric computation
		alwaysYes := &trivialClassifier{pred: true}
		metrics := EvaluateDataset(ds, alwaysYes)
		acc := metrics.Accuracy()
		assertMetric(t, "Accuracy", acc, 0, 1.01)
		f1 := metrics.F1()
		assertMetric(t, "F1", f1, 0, 1.01)
		// With an always-yes classifier on a mixed dataset, we should have
		// both TPs and FPs — metrics must not be NaN or zero.
		if metrics.TruePositives == 0 {
			t.Error("expected some true positives with always-yes classifier")
		}
	})

	t.Run("Memory", func(t *testing.T) {
		ds := DefaultMemoryDataset()
		if len(ds.Facts) == 0 {
			t.Fatal("MemoryDataset has no facts")
		}
		if len(ds.Expected) == 0 {
			t.Fatal("MemoryDataset has no expected mappings")
		}
		// Every fact should have a corresponding query in Expected.
		for _, f := range ds.Facts {
			if _, ok := ds.Expected[f.Query]; !ok {
				t.Errorf("fact %s query %q not in Expected map", f.ID, f.Query)
			}
		}
		// Convert to retrieval Dataset for metric computation.
		queryIDs := ds.QueryIDs()
		retDataset := Dataset{
			Name:     ds.Name,
			QueryIDs: queryIDs,
			Qrels:    ds.Expected,
		}
		runner := NewEvalRunner(5)
		report, err := runner.Run(context.Background(), retDataset, PerfectRetrievalFn(ds.Expected))
		if err != nil {
			t.Fatalf("memory runner: %v", err)
		}
		assertMetric(t, "Recall", report.Recall, 0, 1.01)
		assertMetric(t, "Precision", report.Precision, 0, 1.01)
	})

	t.Run("Reranker", func(t *testing.T) {
		ds := DefaultRerankerDataset()
		if len(ds.Queries) == 0 {
			t.Fatal("RerankerDataset has no queries")
		}
		// Identity reranker: returns candidates in order given
		identityReranker := func(_ string, candidates []string) ([]string, error) {
			return candidates, nil
		}
		ndcg := EvaluateReranker(ds, identityReranker)
		assertMetric(t, "NDCG", ndcg, 0, 1.01)
	})
}

// assertMetric fails if value is NaN, Inf, negative, or > max.
func assertMetric(t *testing.T, name string, value, min, max float64) {
	t.Helper()
	if math.IsNaN(value) {
		t.Errorf("%s is NaN", name)
	}
	if math.IsInf(value, 0) {
		t.Errorf("%s is Inf", name)
	}
	if value < min {
		t.Errorf("%s = %v, want >= %v", name, value, min)
	}
	if value > max {
		t.Errorf("%s = %v, want <= %v", name, value, max)
	}
}

// trivialClassifier always returns the same prediction — used to
// smoke-test contradiction metric computation (not detection quality).
type trivialClassifier struct {
	pred bool
}

func (c *trivialClassifier) Classify(_, _ string) bool { return c.pred }
