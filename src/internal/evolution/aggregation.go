package evolution

import "github.com/pavelveter/hermem/src/internal/memory/evidence"

// Aggregator selects the aggregation strategy for evidence strengths.
type Aggregator int

const (
	AggregatorSum  Aggregator = iota // sum of strengths (default)
	AggregatorAvg                    // average of strengths
	AggregatorMin                    // minimum strength
)

// AggregateEvidence groups evidence by polarity and returns aggregated
// strength values using the given selector. Returns 0 for a polarity
// group when no evidence of that type exists.
func AggregateEvidence(all []*evidence.Evidence, selector Aggregator) (support, refute float64) {
	var sSum, rSum float64
	var sCount, rCount int
	var sMin, rMin float64
	firstS, firstR := true, true

	for _, e := range all {
		switch e.Polarity {
		case evidence.PolaritySupport:
			sSum += e.Strength
			sCount++
			if firstS || e.Strength < sMin {
				sMin = e.Strength
				firstS = false
			}
		case evidence.PolarityRefute:
			rSum += e.Strength
			rCount++
			if firstR || e.Strength < rMin {
				rMin = e.Strength
				firstR = false
			}
		}
	}

	switch selector {
	case AggregatorAvg:
		if sCount > 0 {
			support = sSum / float64(sCount)
		}
		if rCount > 0 {
			refute = rSum / float64(rCount)
		}
	case AggregatorMin:
		if !firstS {
			support = sMin
		}
		if !firstR {
			refute = rMin
		}
	default:
		support = sSum
		refute = rSum
	}
	return
}
