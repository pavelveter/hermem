package evolution

import "github.com/pavelveter/hermem/src/internal/memory/evidence"

// Aggregator selects the aggregation strategy for evidence strengths.
type Aggregator int

const (
	AggregatorSum Aggregator = iota // sum of strengths (default)
	AggregatorAvg                   // average of strengths
	AggregatorMin                   // minimum strength
)

// AggregateEvidence groups evidence by polarity and returns aggregated
// strength values using the given selector. Returns 0 for a polarity
// group when no evidence of that type exists.
func AggregateEvidence(all []*evidence.Evidence, selector Aggregator) (support, refute float64) {
	support = aggregatePolarity(all, evidence.PolaritySupport, selector)
	refute = aggregatePolarity(all, evidence.PolarityRefute, selector)
	return
}

// aggregatePolarity computes the aggregate strength for a single polarity.
func aggregatePolarity(all []*evidence.Evidence, pol evidence.Polarity, selector Aggregator) float64 {
	var sum float64
	var count int
	var min float64
	first := true

	for _, e := range all {
		if e.Polarity != pol {
			continue
		}
		sum += e.Strength
		count++
		if first || e.Strength < min {
			min = e.Strength
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
