package core

// Episode captures the ingestion provenance of a Fact — the conversation
// and message it was extracted from, plus a free-form pointer to the
// source span. Identity (ID + Content + Category + Embedding) is
// supplied by the embedded Fact so /episode/* endpoints can
// serialise Episode directly without going through fat Entity
// (see §8 of REFACTORING.md). The Episode-specific fields here
// capture only the provenance triple (ConversationID, MessageID,
// ExtractedFrom); quality / lifecycle / persistence mechanics
// live on Evidence / Task / Belief respectively.
//
// P0 ENTITY MODEL REFACTOR (item #5) — lands after Fact (item #3) and
// Evidence (item #4). Pattern matches both: minimal surface, identity
// via embedded Fact, provenance triple as the explicit struct fields.
// The AsEpisode() conversion keeps working for callers that prefer
// the per-domain-model API; the projection becomes dead code once
// all consumers use slim types directly (§8 Phase 4).
type Episode struct {
	Fact
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
//
// §8 NOTE: drops embedded Fact identity (ID/Category/Content/Embedding)
// silently. Until §8 Phase 2 (read-path switchover) lands, callers
// that round-trip through AsEntity lose identity — prefer consuming
// the slim type directly when both identity and domain-specific
// fields are needed.
func (ep Episode) AsEntity() Entity {
	return Entity{
		ConversationID: ep.ConversationID,
		MessageID:      ep.MessageID,
		ExtractedFrom:  ep.ExtractedFrom,
	}
}
