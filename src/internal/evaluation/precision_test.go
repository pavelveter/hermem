package evaluation

import (
	"testing"
)

func TestPrecision_Perfect(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2"},
	}
	results := map[string][]string{
		"q1": {"d1", "d2", "d5"},
	}
	got := Precision(qrels, results, 2)
	if got != 1.0 {
		t.Fatalf("Precision@2 = %v, want 1.0", got)
	}
}

func TestPrecision_Partial(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2"},
	}
	results := map[string][]string{
		"q1": {"d1", "d3"},
	}
	// 1 relevant out of 2 retrieved → 1/2
	got := Precision(qrels, results, 2)
	if got != 0.5 {
		t.Fatalf("Precision@2 = %v, want 0.5", got)
	}
}

func TestPrecision_EmptyQrels(t *testing.T) {
	got := Precision(nil, nil, 10)
	if got != 0 {
		t.Fatalf("Precision@10 = %v, want 0", got)
	}
}

func TestPrecision_ZeroK(t *testing.T) {
	qrels := map[string][]string{"q1": {"d1"}}
	got := Precision(qrels, nil, 0)
	if got != 0 {
		t.Fatalf("Precision@0 = %v, want 0", got)
	}
}

func TestPrecision_NoResults(t *testing.T) {
	qrels := map[string][]string{"q1": {"d1"}}
	got := Precision(qrels, nil, 10)
	if got != 0 {
		t.Fatalf("Precision@10 = %v, want 0", got)
	}
}

func TestPrecision_MultiQuery(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2"},
		"q2": {"d3"},
	}
	results := map[string][]string{
		"q1": {"d1", "d4"}, // 1/2
		"q2": {"d3", "d5"}, // 1/2
	}
	// total relevant = 2, total retrieved = 4 → 0.5
	got := Precision(qrels, results, 5)
	if got != 0.5 {
		t.Fatalf("Precision@5 = %v, want 0.5", got)
	}
}
