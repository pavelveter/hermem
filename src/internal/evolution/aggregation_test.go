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
