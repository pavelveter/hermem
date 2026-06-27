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

// ComposeFromTask reassembles a Task into a fat Entity at non-wire
// boundaries that need the full Entity shape (e.g. orchestrator's
// internal execFunc callback). The other 4 bands are zero-valued, so
// this is NOT safe for HTTP wire serialization — that path uses the
// slim Task directly per the §8.1 Task-shell design intent.
// Inline copy keeps the band-zeroing explicit at each call site
// instead of duplicating `core.Compose(t.Fact, …, t, …)` everywhere.
func ComposeFromTask(t Task) Entity {
	return Compose(t.Fact, Evidence{}, Episode{}, t, Belief{})
}

// WithInitialStatus returns a copy of t with Status set to the first
// valid state from schema.ValidStateOrder when Status is empty.
// This centralizes the "stateful entities start at the first valid state"
// rule that was previously duplicated in store/entity.go, ingestion/worker.go.
func (t Task) WithInitialStatus(schema SchemaConfig) Task {
	if t.Status == "" && schema.StatefulCategories[t.Category] && len(schema.ValidStateOrder) > 0 {
		t.Status = schema.ValidStateOrder[0]
	}
	return t
}

// CanTransitionTo reports whether transitioning from t.Status to newStatus
// is allowed by the schema's ValidStateOrder. Adjacent states (next or
// previous in the order) are always allowed. Non-adjacent transitions
// return false.
func (t Task) CanTransitionTo(newStatus string, schema SchemaConfig) bool {
	if !schema.ValidStates[newStatus] {
		return false
	}
	if len(schema.ValidStateOrder) == 0 {
		return true
	}
	for i, s := range schema.ValidStateOrder {
		if s == t.Status {
			if i > 0 && schema.ValidStateOrder[i-1] == newStatus {
				return true
			}
			if i < len(schema.ValidStateOrder)-1 && schema.ValidStateOrder[i+1] == newStatus {
				return true
			}
			return false
		}
	}
	return false
}

// TaskClaimRequest is the request body for POST /task/claim-next.
type TaskClaimRequest struct {
	GoalID string `json:"goal_id,omitempty"`
}

// TaskClaimResponse is the response body for POST /task/claim-next.
type TaskClaimResponse struct {
	Task *Task `json:"task,omitempty"`
}
