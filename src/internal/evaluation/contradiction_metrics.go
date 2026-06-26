package evaluation

// ContradictionMetrics holds standard classification metrics for
// contradiction detection evaluation.
type ContradictionMetrics struct {
	TruePositives  int
	FalsePositives int
	TrueNegatives  int
	FalseNegatives int
}

// Accuracy returns (TP + TN) / (TP + TN + FP + FN).
func (m ContradictionMetrics) Accuracy() float64 {
	total := m.TruePositives + m.TrueNegatives + m.FalsePositives + m.FalseNegatives
	if total == 0 {
		return 0
	}
	return float64(m.TruePositives+m.TrueNegatives) / float64(total)
}

// Precision returns TP / (TP + FP).
func (m ContradictionMetrics) Precision() float64 {
	denom := m.TruePositives + m.FalsePositives
	if denom == 0 {
		return 0
	}
	return float64(m.TruePositives) / float64(denom)
}

// Recall returns TP / (TP + FN).
func (m ContradictionMetrics) Recall() float64 {
	denom := m.TruePositives + m.FalseNegatives
	if denom == 0 {
		return 0
	}
	return float64(m.TruePositives) / float64(denom)
}

// F1 returns the harmonic mean of Precision and Recall.
func (m ContradictionMetrics) F1() float64 {
	p, r := m.Precision(), m.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// EvaluateContradictionDetector runs a detector against a labeled
// dataset and returns classification metrics.
type ContradictionClassifier interface {
	// Classify returns true if the pair is judged as a contradiction.
	Classify(textA, textB string) bool
}

// EvaluateDataset runs classifier on dataset and returns metrics.
func EvaluateDataset(dataset ContradictionDataset, classifier ContradictionClassifier) ContradictionMetrics {
	var m ContradictionMetrics
	for _, pair := range dataset.Pairs {
		predicted := classifier.Classify(pair.TextA, pair.TextB)
		switch {
		case predicted && pair.IsContradiction:
			m.TruePositives++
		case predicted && !pair.IsContradiction:
			m.FalsePositives++
		case !predicted && pair.IsContradiction:
			m.FalseNegatives++
		case !predicted && !pair.IsContradiction:
			m.TrueNegatives++
		}
	}
	return m
}
