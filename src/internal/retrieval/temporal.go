package retrieval

import (
	"math"
	"time"
)

// This file owns the retrieval pipeline's temporal ranking stage —
// the score component that decays with how old a fact is. Pulled
// out of scoring.go so the temporal concern is file-isolated and a
// reader scanning retrieval/ can see it as its own primitive, just
// like graph expansion lives in expand.go.
//
// temporalScore is one of the four raw features inside
// core.ScoreBreakdown (the other three — vector similarity, recency,
// centrality — stay in scoring.go because they are the ranker's
// primary signals; temporal is its own axis).

// expDecayHours is the single canonical implementation of the
// exp(-hoursOld/halfLife) decay used by recencyScore and temporalScore.
// Both functions used to inline the same formula (with subtle
// differences only in their empty-input default) — keep them thin
// wrappers over this helper so the math stays in lockstep.
//
// Lives in temporal.go because exp decay is the temporal ranking
// primitive; recencyScore (scoring.go) reuses it for the UpdatedAt
// axis via the same-package call.
func expDecayHours(ts time.Time, halfLifeHours float32) float32 {
	if ts.IsZero() || halfLifeHours <= 0 {
		return 0
	}
	hoursOld := float32(time.Since(ts.UTC()).Hours())
	if hoursOld <= 0 {
		return 1
	}
	return float32(math.Exp(-float64(hoursOld) / float64(halfLifeHours)))
}

// temporalScore — exp-decay on CreatedAt. Nil/zero CreatedAt → 0
// ("no temporal signal"), distinct from recency's default because an
// unknown creation time should not contribute to the temporal boost.
//
// halfLifeHours comes from RankingWeight.TemporalHalfLifeHours
// (default 720 = 30 days).
func temporalScore(createdAt *time.Time, halfLifeHours float32) float32 {
	if createdAt == nil || createdAt.IsZero() || halfLifeHours <= 0 {
		return 0
	}
	return expDecayHours(*createdAt, halfLifeHours)
}
