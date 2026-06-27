package core

import "time"

// Belief is the persistence / retention / graph-anchor view of a Fact
// in the hermem knowledge graph. It owns the 5 mechanical fields that
// govern HOW a fact lives in memory:
//   - CreatedAt + UpdatedAt — when the fact was first / last written
//   - LastAccessedAt — TTL/GC tick (retention.Service scans on this)
//   - Archived — GC retention mark (Archived=true excluded from
//     graph walks)
//   - Degree — graph centrality (auto-maintained by SQL triggers on
//     edges INSERT/DELETE; the ranker uses log10(1+degree))
//
// Identity (ID + Content + Category + Embedding) is supplied by the
// embedded Fact so /belief/* endpoints can serialise Belief
// directly without going through fat Entity (see §8 of REFACTORING.md).
//
// IMPORTANT: "Belief" in this codebase does NOT mean the philosophical
// concept of a held claim. It means the persistent memory footprint
// of a fact — the long-arc, graph-anchored, retention-tracked block
// that distinguishes a fact-as-stored-row from a fact-as-content.
// The name is the TODO item; do not extend it to mean a domain-level
// confidence aggregation (that is Evidence's job).
//
// P0 ENTITY MODEL REFACTOR (item #8) — closes the model split. After
// this commit, all 19 Entity fields are distributed across the 5
// concrete siblings (Fact/Evidence/Episode/Task/Belief) plus the
// derived Task-typed view (Goal). Entity stays as the umbrella
// persistence-row type and the per-domain models compose into it.
//
// Pattern matches: minimal surface, identity via embedded Fact,
// persistence mechanics as the explicit struct fields. The AsBelief()
// down-direction projection (Entity → slim type) is the canonical
// API; the inverse Belief.AsEntity() (lossy on embedded-Fact
// identity) was removed in §8.4.
type Belief struct {
	Fact
	CreatedAt      *time.Time `json:"created_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at"`
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`
	Archived       bool       `json:"archived,omitempty"`
	Degree         int        `json:"degree,omitempty"`
}

// AsBelief pulls the 5 belief fields off an Entity and discards the
// remaining 14 (Fact/Evidence/Episode/Task fields + ID/Category).
// Callers that need the full row continue to use Entity.
func (e Entity) AsBelief() Belief {
	return Belief{
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
		LastAccessedAt: e.LastAccessedAt,
		Archived:       e.Archived,
		Degree:         e.Degree,
	}
}
