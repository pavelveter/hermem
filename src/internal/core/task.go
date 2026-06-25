package core

import "time"

// Task captures the stateful-lifecycle meta-block attached to an
// Entity when its category is stateful (one of schema.StatefulCategories).
// It carries the current lifecycle status, validity window, and
// priority — none of which make sense on a plain Fact. It carries NO
// identity / content / quality / provenance / graph fields.
//
// P0 ENTITY MODEL REFACTOR (item #6) — lands after Fact (item #3),
// Evidence (item #4), Episode (item #5). The pattern matches all
// three: minimal surface, Entity keeps its existing fields, conversion
// methods project and round-trip cleanly for callers that prefer the
// per-domain-model API.
type Task struct {
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
func (t Task) AsEntity() Entity {
	return Entity{
		Status:    t.Status,
		ValidFrom: t.ValidFrom,
		ValidTo:   t.ValidTo,
		Priority:  t.Priority,
	}
}
