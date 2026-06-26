package core

import "time"

// Task captures the stateful-lifecycle meta-block attached to an
// Entity when its category is stateful (one of schema.StatefulCategories).
// It carries the current lifecycle status, validity window, and
// priority — none of which make sense on a plain Fact. It picks up
// identity (ID + Content + Category + Embedding) from the embedded
// Fact so /task/list + /task/show can serialise Task directly
// without going through fat Entity (see §8 of REFACTORING.md).
//
// P0 ENTITY MODEL REFACTOR (item #6) — lands after Fact (item #3),
// Evidence (item #4), Episode (item #5). The pattern matches all
// three: minimal domain-specific surface, identity via embedded Fact,
// lifecycle metadata as the explicit struct fields. The AsTask() /
// AsEntity() conversion methods keep working for callers that prefer
// the per-domain-model API; the AsEntity() projection becomes dead
// code once all consumers use slim types directly (§8 Phase 4).
type Task struct {
	Fact
	Status    string     `json:"status,omitempty"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	Priority  int        `json:"priority,omitempty"`
}

// AsTask pulls the 4 task-lifecycle fields off an Entity and discards
// the remaining 15 (ID / Category / Content / Embedding / Confidence /
// Source / SourceType / CreatedAt / UpdatedAt / LastAccessedAt /
// Archived / Degree / ConversationID / MessageID / ExtractedFrom).
// Callers that need the full row continue to use Entity.
//
// §8 TODO: this direction round-trips identity (Ent fields ID/Category/
// Content/Embedding drop into the embedded Task.Fact automatically —
// they aren't explicitly mapped). The INVERSE Task.AsEntity() does NOT —
// it constructs a fresh Entity and silently drops the embedded Fact.
// §8 Phase 2 must replace callers doing `t.AsEntity()` with `t` directly.
// No static guard exists; pre-Phase-2 callers that read .ID after an
// AsEntity round-trip will get the zero value silently.
func (e Entity) AsTask() Task {
	return Task{
		Status:    e.Status,
		ValidFrom: e.ValidFrom,
		ValidTo:   e.ValidTo,
		Priority:  e.Priority,
	}
}

// AsEntity lifts the 4 task-lifecycle fields back into an Entity.
// The 15 non-task fields are zeroed / nil. Callers that need them
// back must merge from a domain-specific source (Fact for content,
// Evidence for quality, Episode for provenance, etc.).
//
// §8 NOTE: drops embedded Fact identity (ID/Category/Content/Embedding)
// silently. Until §8 Phase 2 (read-path switchover) lands, callers
// that round-trip through AsEntity lose identity — prefer consuming
// the slim type directly when both identity and domain-specific
// fields are needed.
func (t Task) AsEntity() Entity {
	return Entity{
		Status:    t.Status,
		ValidFrom: t.ValidFrom,
		ValidTo:   t.ValidTo,
		Priority:  t.Priority,
	}
}
