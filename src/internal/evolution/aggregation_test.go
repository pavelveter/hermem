package evolution

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/memory/evidence"
)

func TestAggregateEvidence_Sum(t *testing.T) {
	all := []*evidence.Evidence{
		{Polarity: evidence.PolaritySupport, Strength: 0.5},
		{Polarity: evidence.PolaritySupport, Strength: 0.3},
		{Polarity: evidence.PolarityRefute, Strength: 0.2},
	}
	s, r := AggregateEvidence(all, AggregatorSum)
	if roundTo(s, 4) != 0.8 {
		t.Errorf("expected support=0.8, got %f", s)
	}
	if roundTo(r, 4) != 0.2 {
		t.Errorf("expected refute=0.2, got %f", r)
	}
}

func TestAggregateEvidence_Avg(t *testing.T) {
	all := []*evidence.Evidence{
		{Polarity: evidence.PolaritySupport, Strength: 0.8},
		{Polarity: evidence.PolaritySupport, Strength: 0.4},
		{Polarity: evidence.PolarityRefute, Strength: 0.6},
	}
	s, r := AggregateEvidence(all, AggregatorAvg)
	if roundTo(s, 4) != 0.6 {
		t.Errorf("expected support avg=0.6, got %f", s)
	}
	if roundTo(r, 4) != 0.6 {
		t.Errorf("expected refute avg=0.6, got %f", r)
	}
}

func TestAggregateEvidence_Min(t *testing.T) {
	all := []*evidence.Evidence{
		{Polarity: evidence.PolaritySupport, Strength: 0.9},
		{Polarity: evidence.PolaritySupport, Strength: 0.3},
		{Polarity: evidence.PolarityRefute, Strength: 0.7},
		{Polarity: evidence.PolarityRefute, Strength: 0.1},
	}
	s, r := AggregateEvidence(all, AggregatorMin)
	if roundTo(s, 4) != 0.3 {
		t.Errorf("expected support min=0.3, got %f", s)
	}
	if roundTo(r, 4) != 0.1 {
		t.Errorf("expected refute min=0.1, got %f", r)
	}
}

func TestAggregateEvidence_Empty(t *testing.T) {
	s, r := AggregateEvidence(nil, AggregatorSum)
	if s != 0 || r != 0 {
		t.Errorf("expected 0, got support=%f refute=%f", s, r)
	}
}

// TestAggregateEvidence_TableDriven covers all polarity/selector combinations.
func TestAggregateEvidence_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		evidence   []*evidence.Evidence
		selector   Aggregator
		wantSup    float64
		wantRef    float64
	}{
		{
			name:     "support_only_sum",
			evidence: []*evidence.Evidence{{Polarity: evidence.PolaritySupport, Strength: 0.3}, {Polarity: evidence.PolaritySupport, Strength: 0.7}},
			selector: AggregatorSum, wantSup: 1.0, wantRef: 0,
		},
		{
			name:     "refute_only_avg",
			evidence: []*evidence.Evidence{{Polarity: evidence.PolarityRefute, Strength: 0.4}, {Polarity: evidence.PolarityRefute, Strength: 0.8}},
			selector: AggregatorAvg, wantSup: 0, wantRef: 0.6,
		},
		{
			name: "mixed_min",
			evidence: []*evidence.Evidence{
				{Polarity: evidence.PolaritySupport, Strength: 0.5},
				{Polarity: evidence.PolarityRefute, Strength: 0.1},
			},
			selector: AggregatorMin, wantSup: 0.5, wantRef: 0.1,
		},
		{
			name:     "empty",
			evidence: nil,
			selector: AggregatorSum, wantSup: 0, wantRef: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, r := AggregateEvidence(tt.evidence, tt.selector)
			if roundTo(s, 6) != roundTo(tt.wantSup, 6) {
				t.Errorf("support: want %f, got %f", tt.wantSup, s)
			}
			if roundTo(r, 6) != roundTo(tt.wantRef, 6) {
				t.Errorf("refute: want %f, got %f", tt.wantRef, r)
			}
		})
	}
}
