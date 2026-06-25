package evaluation

import (
	"testing"
)

func TestMRR_FirstRank(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1"},
	}
	results := map[string][]string{
		"q1": {"d1", "d2"},
	}
	got := MRR(qrels, results)
	if got != 1.0 {
		t.Fatalf("MRR = %v, want 1.0", got)
	}
}

func TestMRR_SecondRank(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1"},
	}
	results := map[string][]string{
		"q1": {"d2", "d1"},
	}
	got := MRR(qrels, results)
	if got != 0.5 {
		t.Fatalf("MRR = %v, want 0.5", got)
	}
}

func TestMRR_NoRelevant(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1"},
	}
	results := map[string][]string{
		"q1": {"d2", "d3"},
	}
	got := MRR(qrels, results)
	if got != 0 {
		t.Fatalf("MRR = %v, want 0", got)
	}
}

func TestMRR_MultiQuery(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1"}, // rank 1 → 1/1
		"q2": {"d2"}, // rank 3 → 1/3
	}
	results := map[string][]string{
		"q1": {"d1", "d3"},
		"q2": {"d5", "d6", "d2"},
	}
	got := MRR(qrels, results)
	want := (1.0 + 1.0/3.0) / 2.0
	if got != want {
		t.Fatalf("MRR = %v, want %v", got, want)
	}
}

func TestMRR_EmptyQrels(t *testing.T) {
	got := MRR(nil, nil)
	if got != 0 {
		t.Fatalf("MRR = %v, want 0", got)
	}
}

func TestMRR_EmptyResults(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1"},
	}
	got := MRR(qrels, nil)
	if got != 0 {
		t.Fatalf("MRR = %v, want 0", got)
	}
}
