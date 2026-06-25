package evaluation

import (
	"math"
	"testing"
)

func TestNDCG_Perfect(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2"},
	}
	results := map[string][]string{
		"q1": {"d1", "d2", "d5"},
	}
	got := NDCG(qrels, results, 3)
	if got != 1.0 {
		t.Fatalf("NDCG@3 = %v, want 1.0", got)
	}
}

func TestNDCG_Partial(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2"},
	}
	results := map[string][]string{
		"q1": {"d1", "d3"}, // d1 at rank 1, d2 not found
	}
	dcg := 1.0 / math.Log2(2) // d1 at rank 1
	idcg := 1.0/math.Log2(2) + 1.0/math.Log2(3)
	got := NDCG(qrels, results, 2)
	want := dcg / idcg
	if got != want {
		t.Fatalf("NDCG@2 = %v, want %v", got, want)
	}
}

func TestNDCG_EmptyQrels(t *testing.T) {
	got := NDCG(nil, nil, 10)
	if got != 0 {
		t.Fatalf("NDCG@10 = %v, want 0", got)
	}
}

func TestNDCG_ZeroK(t *testing.T) {
	qrels := map[string][]string{"q1": {"d1"}}
	got := NDCG(qrels, nil, 0)
	if got != 0 {
		t.Fatalf("NDCG@0 = %v, want 0", got)
	}
}

func TestNDCG_NoRelevant(t *testing.T) {
	qrels := map[string][]string{
		"q1": {},
	}
	got := NDCG(qrels, nil, 10)
	if got != 0 {
		t.Fatalf("NDCG@10 = %v, want 0", got)
	}
}

func TestNDCG_MultiQuery(t *testing.T) {
	qrels := map[string][]string{
		"q1": {"d1", "d2"}, // perfect → 1.0
		"q2": {"d1"},       // d1 at rank 1 → 1.0 / log2(2) / (1.0 / log2(2)) = 1.0
	}
	results := map[string][]string{
		"q1": {"d1", "d2"},
		"q2": {"d1", "d3"},
	}
	got := NDCG(qrels, results, 3)
	if got != 1.0 {
		t.Fatalf("NDCG@3 = %v, want 1.0", got)
	}
}
