package domain

// Fact is the smallest domain model representing a semantic claim.
type Fact struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

// AsFact projects an Entity down to its semantic claim fields.
func (e Entity) AsFact() Fact {
	return Fact{ID: e.ID, Category: e.Category, Content: e.Content, Embedding: e.Embedding}
}

// AsEntity lifts a Fact into an Entity with zero-valued metadata fields.
func (f Fact) AsEntity() Entity {
	return Entity{ID: f.ID, Category: f.Category, Content: f.Content, Embedding: f.Embedding}
}
