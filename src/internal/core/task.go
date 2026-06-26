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
// lifecycle metadata as the explicit struct fields. The AsTask()
// down-direction projection (Entity → slim type) is the canonical
// API; the inverse Task.AsEntity() (lossy on embedded-Fact identity)
// was removed in §8.4.
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
// This direction round-trips identity: the source Entity's ID /
// Category / Content / Embedding land in the embedded Task.Fact
// automatically via Go's anon-embed promotion (no explicit mapping).
// The inverse Task.AsEntity() was lossy on the embedded Fact and
// was removed in §8.4.
func (e Entity) AsTask() Task {
	return Task{
		Status:    e.Status,
		ValidFrom: e.ValidFrom,
		ValidTo:   e.ValidTo,
		Priority:  e.Priority,
	}
}

