package core

// Fact is the smallest domain model representing a semantic claim.
// It captures only the identity, basic classification, content, and
// vector representation of a unit of knowledge — nothing machine-state,
// nothing provenance, nothing retention machinery.
//
// P0 ENTITY MODEL REFACTOR (item #3) — Evidence, Episode, Task, Goal,
// Belief models come in subsequent commits. Entity stays as the
// umbrella persistence-view type; callers continue to use Entity for
// DB/retrieval/wire-format work and can move to Fact over time as the
// per-domain models land.
type Fact struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

// AsFact projects an Entity down to the Fact essence. The 15 metadata
// fields (Status / ValidFrom..To / Confidence / Source / SourceType /
// CreatedAt / UpdatedAt / LastAccessedAt / Archived / Degree /
// Priority / ConversationID / MessageID / ExtractedFrom) are dropped —
// they belong to other domain models (Task, Evidence, Episode) or to
// graph mechanics, not to a Fact itself.
func (e Entity) AsFact() Fact {
	return Fact{
		ID:        e.ID,
		Category:  e.Category,
		Content:   e.Content,
		Embedding: e.Embedding,
	}
}

// AsEntity lifts a Fact back into an Entity. The 15 metadata fields
// get their zero/nil defaults; callers who need them back must supply
// them from a domain-specific source (e.g. Task lifecycle, Episode
// provenance, retention last-accessed timer).
func (f Fact) AsEntity() Entity {
	return Entity{
		ID:        f.ID,
		Category:  f.Category,
		Content:   f.Content,
		Embedding: f.Embedding,
	}
}
