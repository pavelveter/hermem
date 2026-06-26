package core

import "time"

// Goal captures the lifecycle state of a goal — a goal IS A Task in
// this schema, distinguished only by Entity.Category == "goal" at
// the parent row. The struct mirrors Task's field shape exactly so
// callers can use Goal vs Task interchangeably (e.g. Goal.AsTask() /
// Task.AsGoal()) without losing any field. Identity (ID + Content +
// Category + Embedding) is supplied by the embedded Fact so
// /goal/list + /goal/show can serialise Goal directly without going
// through fat Entity (see §8 of REFACTORING.md).
//
// Subgoal hierarchy is NOT modeled here: it lives in the graph via
// blocked_by edges. ProgressPercent is NOT modeled here: it is
// derived from counting completed child tasks during traversal.
// CompletedAt is NOT modeled here: it is the parent Entity's
// ValidTo when Status=="completed".
//
// P0 ENTITY MODEL REFACTOR (item #7) — lands after Fact (item #3),
// Evidence (item #4), Episode (item #5), Task (item #6). Pattern
// matches: minimal surface, identity via embedded Fact, lifecycle
// metadata as the explicit struct fields. Stored rows distinguish
// Goal from Task only by their category column at SQL layer.
type Goal struct {
	Fact
	Status    string     `json:"status,omitempty"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	Priority  int        `json:"priority,omitempty"`
}

// AsGoal lifts a 4-field goal-shaped projection off an Entity.
// No Category=="goal" guard here — the projection is a dumb field
// map and lets the service layer enforce category semantics.
func (e Entity) AsGoal() Goal {
	return Goal{
		Status:    e.Status,
		ValidFrom: e.ValidFrom,
		ValidTo:   e.ValidTo,
		Priority:  e.Priority,
	}
}

// AsEntity lifts the 4 goal fields back into an Entity. The 15
// non-goal fields are zeroed / nil. Callers that need them back
// must merge from a domain-specific source (Fact for content,
// Evidence for quality, Episode for provenance, etc.).
//
// §8 NOTE: drops embedded Fact identity (ID/Category/Content/Embedding)
// silently. Until §8 Phase 2 (read-path switchover) lands, callers
// that round-trip through AsEntity lose identity — prefer consuming
// the slim type directly when both identity and domain-specific
// fields are needed.
func (g Goal) AsEntity() Entity {
	return Entity{
		Status:    g.Status,
		ValidFrom: g.ValidFrom,
		ValidTo:   g.ValidTo,
		Priority:  g.Priority,
	}
}
