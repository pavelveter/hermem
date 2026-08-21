package domain

import "time"

// Belief captures persistence, retention, and graph-anchor metadata.
type Belief struct {
	Fact
	CreatedAt      *time.Time `json:"created_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at"`
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`
	Archived       bool       `json:"archived,omitempty"`
	Degree         int        `json:"degree,omitempty"`
}

// AsBelief projects an Entity into its persistence metadata.
func (e Entity) AsBelief() Belief {
	return Belief{
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		LastAccessedAt: e.LastAccessedAt, Archived: e.Archived, Degree: e.Degree,
	}
}
