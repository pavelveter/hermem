package evaluation

import (
	"strings"
	"testing"
	"time"
)

func TestReport_Format(t *testing.T) {
	r := Report{
		Dataset:      "test-ds",
		Recall:       0.85,
		Precision:    0.72,
		MRR:          0.91,
		NDCG:         0.88,
		TotalQueries: 100,
		K:            10,
		RunAt:        time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}

	s := r.Format()
	if !strings.Contains(s, "=== Benchmark Report ===") {
		t.Fatal("Format missing header")
	}
	if !strings.Contains(s, "Dataset:      test-ds") {
		t.Fatal("Format missing dataset")
	}
	if !strings.Contains(s, "Recall@10:    0.8500") {
		t.Fatal("Format missing Recall")
	}
	if !strings.Contains(s, "Precision@10: 0.7200") {
		t.Fatal("Format missing Precision")
	}
	if !strings.Contains(s, "MRR:          0.9100") {
		t.Fatal("Format missing MRR")
	}
	if !strings.Contains(s, "NDCG@10:      0.8800") {
		t.Fatal("Format missing NDCG")
	}
}

func TestReport_JSON(t *testing.T) {
	r := Report{
		Dataset:      "test",
		Recall:       0.5,
		Precision:    0.6,
		MRR:          0.7,
		NDCG:         0.8,
		TotalQueries: 10,
		K:            5,
		RunAt:        time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}

	data := r.JSON()
	s := string(data)
	if !strings.Contains(s, `"dataset": "test"`) {
		t.Fatalf("JSON missing dataset, got: %s", s)
	}
	if !strings.Contains(s, `"recall": 0.5`) {
		t.Fatalf("JSON missing recall, got: %s", s)
	}
	if !strings.Contains(s, `"total_queries": 10`) {
		t.Fatalf("JSON missing total_queries, got: %s", s)
	}
}
