// Package evolution implements cross-cutting logic for P2 MEMORY EVOLUTION:
// confidence propagation, evidence aggregation, trust scoring, belief revision
// chains, superseded beliefs, support/refute relationships, history tracking,
// and evolution queries.
package evolution

import (
	"context"
	"fmt"
	"math"

	"github.com/pavelveter/hermem/src/internal/memory/belief"
	"github.com/pavelveter/hermem/src/internal/memory/evidence"
)

// PropagateConfidence aggregates all evidence for a belief by polarity and
// updates the belief's confidence to the ratio of total support strength
// over total evidence strength.
//
// Formula:
//
//	support = sum(strength) over evidence where polarity='support'
//	refute  = sum(strength) over evidence where polarity='refute'
//	total   = support + refute
//	confidence = clamp(support / total, 0, 1) when total > 0
//	confidence = unchanged (default 1.0) when total == 0
func PropagateConfidence(ctx context.Context, bSvc belief.Service, eSvc evidence.Service, beliefID int64) (float64, error) {
	if beliefID <= 0 {
		return 0, fmt.Errorf("evolution: invalid belief ID %d", beliefID)
	}

	all, err := eSvc.ListForBelief(ctx, beliefID)
	if err != nil {
		return 0, fmt.Errorf("evolution: list evidence: %w", err)
	}

	var supportSum, refuteSum float64
	for _, e := range all {
		switch e.Polarity {
		case evidence.PolaritySupport:
			supportSum += e.Strength
		case evidence.PolarityRefute:
			refuteSum += e.Strength
		}
	}

	total := supportSum + refuteSum
	var newConf float64
	if total > 0 {
		newConf = clamp(supportSum/total, 0, 1)
	} else {
		b, err := bSvc.GetBelief(ctx, beliefID)
		if err != nil {
			return 0, fmt.Errorf("evolution: get belief: %w", err)
		}
		newConf = b.Confidence
	}

	if err := bSvc.UpdateConfidence(ctx, beliefID, newConf); err != nil {
		return 0, fmt.Errorf("evolution: update confidence: %w", err)
	}
	return newConf, nil
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func roundTo(v float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(v*pow) / pow
}
