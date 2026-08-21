package v1

import (
	"time"

	"github.com/pavelveter/hermem/pkg/domain"
)

// EntityDTO is the wire representation of a domain entity. Keeping this
// type separate means transport field changes do not alter domain ownership.
type EntityDTO struct {
	ID             string     `json:"id"`
	Category       string     `json:"category"`
	Content        string     `json:"content"`
	Embedding      []float32  `json:"embedding,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`
	Archived       bool       `json:"archived,omitempty"`
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

func EntityFromDomain(e domain.Entity) EntityDTO {
	return EntityDTO{
		ID: e.ID, Category: e.Category, Content: e.Content, Embedding: e.Embedding,
		UpdatedAt: e.UpdatedAt, LastAccessedAt: e.LastAccessedAt, Archived: e.Archived,
		Status: e.Status, Confidence: e.Confidence, Source: e.Source, SourceType: e.SourceType,
		CreatedAt: e.CreatedAt, ValidFrom: e.ValidFrom, ValidTo: e.ValidTo,
		ConversationID: e.ConversationID, MessageID: e.MessageID, ExtractedFrom: e.ExtractedFrom,
		Degree: e.Degree, Priority: e.Priority,
	}
}

type EdgeDTO struct {
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	Weight       float32 `json:"weight,omitempty"`
}

func EdgeFromDomain(e domain.Edge) EdgeDTO {
	return EdgeDTO{SourceID: e.SourceID, TargetID: e.TargetID, RelationType: e.RelationType, Weight: e.Weight}
}
