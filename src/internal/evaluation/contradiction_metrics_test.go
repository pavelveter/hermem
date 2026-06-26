package evaluation

import "testing"

func TestContradictionMetrics(t *testing.T) {
	cases := []struct {
		name string
		m    ContradictionMetrics
		acc  float64
		prec float64
		rec  float64
		f1   float64
	}{
		{
			name: "perfect",
			m:    ContradictionMetrics{TruePositives: 8, TrueNegatives: 12, FalsePositives: 0, FalseNegatives: 0},
			acc:  1.0, prec: 1.0, rec: 1.0, f1: 1.0,
		},
		{
			name: "all_wrong",
			m:    ContradictionMetrics{TruePositives: 0, TrueNegatives: 0, FalsePositives: 10, FalseNegatives: 10},
			acc:  0, prec: 0, rec: 0, f1: 0,
		},
		{
			name: "mixed",
			m:    ContradictionMetrics{TruePositives: 7, TrueNegatives: 10, FalsePositives: 3, FalseNegatives: 2},
			acc:  17.0 / 22.0, prec: 0.7, rec: 7.0 / 9.0, f1: 2 * 0.7 * (7.0 / 9.0) / (0.7 + 7.0/9.0),
		},
		{
			name: "zero",
			m:    ContradictionMetrics{},
			acc:  0, prec: 0, rec: 0, f1: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.m.Accuracy(); !floatClose(got, c.acc) {
				t.Errorf("Accuracy = %v, want %v", got, c.acc)
			}
			if got := c.m.Precision(); !floatClose(got, c.prec) {
				t.Errorf("Precision = %v, want %v", got, c.prec)
			}
			if got := c.m.Recall(); !floatClose(got, c.rec) {
				t.Errorf("Recall = %v, want %v", got, c.rec)
			}
			if got := c.m.F1(); !floatClose(got, c.f1) {
				t.Errorf("F1 = %v, want %v", got, c.f1)
			}
		})
	}
}

func floatClose(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

// mockClassifier is a test double for ContradictionClassifier.
type mockClassifier struct {
	predict func(a, b string) bool
}

func (m *mockClassifier) Classify(a, b string) bool {
	return m.predict(a, b)
}

func TestEvaluateDataset(t *testing.T) {
	ds := ContradictionDataset{
		Name: "test",
		Pairs: []ContradictionPair{
			{ID: "1", TextA: "a", TextB: "b", IsContradiction: true},
			{ID: "2", TextA: "c", TextB: "d", IsContradiction: false},
			{ID: "3", TextA: "e", TextB: "f", IsContradiction: true},
			{ID: "4", TextA: "g", TextB: "h", IsContradiction: false},
		},
	}

	// Classifier that always says contradiction.
	m := &mockClassifier{predict: func(_, _ string) bool { return true }}
	metrics := EvaluateDataset(ds, m)
	if metrics.TruePositives != 2 {
		t.Errorf("TP = %d, want 2", metrics.TruePositives)
	}
	if metrics.FalsePositives != 2 {
		t.Errorf("FP = %d, want 2", metrics.FalsePositives)
	}
	if metrics.FalseNegatives != 0 {
		t.Errorf("FN = %d, want 0", metrics.FalseNegatives)
	}
	if metrics.TrueNegatives != 0 {
		t.Errorf("TN = %d, want 0", metrics.TrueNegatives)
	}

	// Classifier that always says not contradiction.
	m2 := &mockClassifier{predict: func(_, _ string) bool { return false }}
	metrics2 := EvaluateDataset(ds, m2)
	if metrics2.FalseNegatives != 2 {
		t.Errorf("FN = %d, want 2", metrics2.FalseNegatives)
	}
	if metrics2.TrueNegatives != 2 {
		t.Errorf("TN = %d, want 2", metrics2.TrueNegatives)
	}
}
