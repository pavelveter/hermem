package domain

import "time"

// Task captures lifecycle metadata attached to a stateful entity.
type Task struct {
	Fact
	Status    string     `json:"status,omitempty"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	Priority  int        `json:"priority,omitempty"`
}

// AsTask projects an Entity into its task lifecycle fields.
func (e Entity) AsTask() Task {
	return Task{Status: e.Status, ValidFrom: e.ValidFrom, ValidTo: e.ValidTo, Priority: e.Priority}
}

// ComposeFromTask reassembles a Task into a full Entity at internal
// non-wire boundaries. Missing domain bands remain zero-valued.
func ComposeFromTask(t Task) Entity {
	return Compose(t.Fact, Evidence{}, Episode{}, t, Belief{})
}

// WithInitialStatus returns a copy of t with the first valid schema state.
func (t Task) WithInitialStatus(schema SchemaConfig) Task {
	if t.Status == "" && schema.StatefulCategories[t.Category] && len(schema.ValidStateOrder) > 0 {
		t.Status = schema.ValidStateOrder[0]
	}
	return t
}

// CanTransitionTo reports whether a task can move to an adjacent valid state.
func (t Task) CanTransitionTo(newStatus string, schema SchemaConfig) bool {
	if !schema.ValidStates[newStatus] {
		return false
	}
	if len(schema.ValidStateOrder) == 0 {
		return true
	}
	for i, state := range schema.ValidStateOrder {
		if state != t.Status {
			continue
		}
		if i > 0 && schema.ValidStateOrder[i-1] == newStatus {
			return true
		}
		if i < len(schema.ValidStateOrder)-1 && schema.ValidStateOrder[i+1] == newStatus {
			return true
		}
		return false
	}
	return false
}
