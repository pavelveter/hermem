package evaluation

import (
	"testing"
)

func TestRecall_Perfect(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2"},
		"q2": {"d3"},
	}
	results := map[string][]string{
		"q1": {"d1", "d2", "d5"},
		"q2": {"d3", "d4"},
	}
	got := Recall(qrels, results, 3)
	if got != 1.0 {
		t.Fatalf("Recall@3 = %v, want 1.0", got)
	}
}

func TestRecall_Partial(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2", "d3"},
	}
	results := map[string][]string{
		"q1": {"d1", "d4"},
	}
	// only d1 found out of 3 relevant → 1/3
	got := Recall(qrels, results, 2)
	want := 1.0 / 3.0
	if got != want {
		t.Fatalf("Recall@2 = %v, want %v", got, want)
	}
}

func TestRecall_EmptyQrels(t *testing.T) {
	got := Recall(nil, nil, 10)
	if got != 0 {
		t.Fatalf("Recall@10 = %v, want 0", got)
	}
}

func TestRecall_ZeroK(t *testing.T) {
	qrels := map[string][]string{"q1": {"d1"}}
	got := Recall(qrels, nil, 0)
	if got != 0 {
		t.Fatalf("Recall@0 = %v, want 0", got)
	}
}

func TestRecall_NoRelevant(t *testing.T) {
	qrels := map[string][]string{
		"q1": {},
	}
	got := Recall(qrels, nil, 10)
	if got != 0 {
		t.Fatalf("Recall@10 = %v, want 0", got)
	}
}

func TestRecall_MultiQuery(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2"},
		"q2": {"d3"},
	}
	results := map[string][]string{
		"q1": {"d1"},       // 1/2
		"q2": {"d3", "d4"}, // 1/1
	}
	// total relevant = 3, total found = 2 → 2/3
	got := Recall(qrels, results, 5)
	want := 2.0 / 3.0
	if got != want {
		t.Fatalf("Recall@5 = %v, want %v", got, want)
	}
}
