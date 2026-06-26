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
// The AsEpisode() down-direction projection (Entity → slim type) is
// the canonical API; the inverse Episode.AsEntity() (lossy on
// embedded-Fact identity) was removed in §8.4.
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

