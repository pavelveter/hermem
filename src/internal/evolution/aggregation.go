package evolution

import "github.com/pavelveter/hermem/pkg/domain"

// Aggregator selects the aggregation strategy for evidence strengths.
type Aggregator int

const (
	AggregatorSum Aggregator = iota // sum of strengths (default)
	AggregatorAvg                   // average of strengths
	AggregatorMin                   // minimum strength
)

// EvidenceItem is the narrow interface that evolution needs from evidence
// records. evidence.Evidence satisfies this interface, so callers can
// pass []*evidence.Evidence directly.
type EvidenceItem interface {
	GetPolarity() domain.Polarity
	GetStrength() float64
}

// AggregateEvidence groups evidence by polarity and returns aggregated
// strength values using the given selector. Returns 0 for a polarity
// group when no evidence of that type exists.
func AggregateEvidence(all []EvidenceItem, selector Aggregator) (support, refute float64) {
	support = aggregatePolarity(all, domain.PolaritySupport, selector)
	refute = aggregatePolarity(all, domain.PolarityRefute, selector)
	return
}

// aggregatePolarity computes the aggregate strength for a single polarity.
func aggregatePolarity(all []EvidenceItem, pol domain.Polarity, selector Aggregator) float64 {
	var sum float64
	var count int
	var min float64
	first := true

	for _, e := range all {
		if e.GetPolarity() != pol {
			continue
		}
		sum += e.GetStrength()
		count++
		if first || e.GetStrength() < min {
			min = e.GetStrength()
			first = false
		}
	}

	if count == 0 {
		return 0
	}
	switch selector {
	case AggregatorAvg:
		return sum / float64(count)
	case AggregatorMin:
		return min
	default:
		return sum
	}
}
