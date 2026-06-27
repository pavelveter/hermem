package evaluation

import (
	"context"
	"testing"
)

func TestRunner_ComputesAllMetrics(t *testing.T) {
	dataset := Dataset{
		Name:     "test",
		QueryIDs: []string{"q1", "q2"},
		Qrels: map[string][]string{
			"q1": {"d1", "d2"},
			"q2": {"d3"},
		},
	}

	fn := func(_ context.Context, qid string) ([]string, error) {
		switch qid {
		case "q1":
			return []string{"d1", "d2", "d5"}, nil
		case "q2":
			return []string{"d3", "d4"}, nil
		}
		return nil, nil
	}

	runner := NewRunner(5)
	report, err := runner.Run(t.Context(), dataset, fn)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.Dataset != "test" {
		t.Fatalf("Dataset = %q, want %q", report.Dataset, "test")
	}
	if report.Recall != 1.0 {
		t.Fatalf("Recall = %v, want 1.0", report.Recall)
	}
	if report.Precision != 0.6 {
		t.Fatalf("Precision = %v, want 0.6", report.Precision)
	}
	if report.MRR != 1.0 {
		t.Fatalf("MRR = %v, want 1.0", report.MRR)
	}
	if report.NDCG != 1.0 {
		t.Fatalf("NDCG = %v, want 1.0", report.NDCG)
	}
	if report.TotalQueries != 2 {
		t.Fatalf("TotalQueries = %v, want 2", report.TotalQueries)
	}
	if report.K != 5 {
		t.Fatalf("K = %v, want 5", report.K)
	}
}

func TestRunner_ContextCancelled(t *testing.T) {
	dataset := Dataset{
		Name:     "test",
		QueryIDs: []string{"q1"},
		Qrels:    map[string][]string{"q1": {"d1"}},
	}

	fn := func(ctx context.Context, _ string) ([]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	runner := NewRunner(5)
	_, err := runner.Run(ctx, dataset, fn)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
