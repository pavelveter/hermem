// Package domain contains Hermem's transport- and storage-independent domain values.
package domain

import "time"

// EntityID is the stable identifier of a domain entity.
// It remains an alias during the compatibility migration; ADR-035 owns the
// eventual server/content identity strategy.
type EntityID = string

// TaskID is the stable identifier of a task.
// It remains an alias during the compatibility migration; ADR-035 owns the
// eventual task identity strategy.
type TaskID = string

// Entity is the persistence-compatible domain representation of a memory
// entity. JSON tags are retained temporarily so the core facade and current
// wire callers preserve serialization while api/v1 mappers are introduced.
type Entity struct {
	ID             string     `json:"id"`
	Category       string     `json:"category"`
	Content        string     `json:"content"`
	Embedding      []float32  `json:"embedding,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at"`
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`
	Archived       bool       `json:"archived"`
	Status         string     `json:"status,omitempty"`
	Confidence     float32    `json:"confidence,omitempty"`
	Source         string     `json:"source,omitempty"`
	SourceType     string     `json:"source_type,omitempty"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
	ValidFrom      *time.Time `json:"valid_from,omitempty"`
	ValidTo        *time.Time `json:"valid_to,omitempty"`
	ConversationID string     `json:"conversation_id,omitempty"`
	MessageID      string     `json:"message_id,omitempty"`
	ExtractedFrom  string     `json:"extracted_from,omitempty"`
	Degree         int        `json:"degree,omitempty"`
	Priority       int        `json:"priority,omitempty"`
}

// WithInitialStatus returns a copy of e with Status set to the first valid
// state from schema.ValidStateOrder when Status is empty.
func (e Entity) WithInitialStatus(schema SchemaConfig) Entity {
	if e.Status == "" && schema.StatefulCategories[e.Category] && len(schema.ValidStateOrder) > 0 {
		e.Status = schema.ValidStateOrder[0]
	}
	return e
}

// Edge is a directed relation between two entities.
type Edge struct {
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	Weight       float32 `json:"weight,omitempty"`
}

// SchemaConfig defines the allowed categories, relations, and state machine.
type SchemaConfig struct {
	AllowedCategories   map[string]bool
	AllowedRelations    map[string]bool
	StatefulCategories  map[string]bool
	ValidStates         map[string]bool
	ValidStateOrder     []string
	RelationBlocking    string
	RelationContradicts string
	StateUnblocking     string
	RelationRecovery    string
	StatefulEnabled     bool
	CascadeLimit        int
}

// DefaultSchemaConfig returns a SchemaConfig with built-in defaults.
func DefaultSchemaConfig(stateful bool) SchemaConfig {
	cats := map[string]bool{
		"world": true, "opinion": true, "experience": true, "observation": true,
		"summary": true,
	}
	rels := map[string]bool{
		"prefers": true, "uses": true, "mentions": true, "related_to": true,
		"part_of": true, "causes": true, "contradicts": true,
		"blocked_by": true, "recovers_via": true,
	}
	return SchemaConfig{
		AllowedCategories:   cats,
		AllowedRelations:    rels,
		StatefulCategories:  map[string]bool{},
		ValidStates:         map[string]bool{},
		ValidStateOrder:     nil,
		RelationBlocking:    "blocked_by",
		RelationContradicts: "contradicts",
		StateUnblocking:     "completed",
		RelationRecovery:    "recovers_via",
		StatefulEnabled:     stateful,
	}
}

// RelationDraft describes an extractor relation without making its opaque
// target reference a persistent entity identifier. ADR-035 owns resolution.
type RelationDraft struct {
	TargetRef    string
	RelationType string
}

// EntityDraft is the provider-neutral extraction output before identity and
// persistence policy are applied by the application/domain service.
type EntityDraft struct {
	Category  string
	Content   string
	Relations []RelationDraft
}
