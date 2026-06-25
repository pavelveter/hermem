package core

// Episode captures the ingestion provenance of a Fact — the conversation
// and message it was extracted from, plus a free-form pointer to the
// source span. It carries NO identity / content / quality / lifecycle
// fields — those live on Fact / Evidence / Task respectively.
//
// P0 ENTITY MODEL REFACTOR (item #5) — lands after Fact (item #3) and
// Evidence (item #4). The pattern matches both: minimal surface, Entity
// keeps all 16 metadata fields it already has, conversion methods
// project and round-trip cleanly for callers that prefer the
// per-domain-model API.
type Episode struct {
	ConversationID string `json:"conversation_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
	ExtractedFrom  string `json:"extracted_from,omitempty"`
}

// AsEpisode pulls the 3 episode fields off an Entity and discards the
// remaining 16 (Status / ValidFrom..To / CreatedAt / UpdatedAt /
// LastAccessedAt / Archived / Degree / Priority / ID / Category /
// Content / Embedding / Confidence / Source / SourceType). Callers
// that need the full row continue to use Entity.
func (e Entity) AsEpisode() Episode {
	return Episode{
		ConversationID: e.ConversationID,
		MessageID:      e.MessageID,
		ExtractedFrom:  e.ExtractedFrom,
	}
}

// AsEntity lifts the 3 episode fields back into an Entity. The 16
// non-episode fields are zeroed / nil. Callers that need them back
// must merge from a domain-specific source (Fact for content,
// Evidence for quality, Task for lifecycle, etc.).
func (ep Episode) AsEntity() Entity {
	return Entity{
		ConversationID: ep.ConversationID,
		MessageID:      ep.MessageID,
		ExtractedFrom:  ep.ExtractedFrom,
	}
}
