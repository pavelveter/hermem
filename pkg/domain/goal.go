package domain

import "time"

// Goal is a task-shaped lifecycle view distinguished by its category.
type Goal struct {
	Fact
	Status    string     `json:"status,omitempty"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	Priority  int        `json:"priority,omitempty"`
}

// AsGoal projects an Entity into a goal-shaped lifecycle view.
func (e Entity) AsGoal() Goal {
	return Goal{Status: e.Status, ValidFrom: e.ValidFrom, ValidTo: e.ValidTo, Priority: e.Priority}
}
