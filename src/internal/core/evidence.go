package core

// Evidence is the quality / origin meta-block attached to a Fact or an
// Entity. It captures the confidence assigned to the underlying claim,
// the source identifier (= where it came from), and the source type
// classifier (= what kind of source). Identity (ID + Content +
// Category + Embedding) is supplied by the embedded Fact so
// /evidence/* endpoints can serialise Evidence directly without
// going through fat Entity (see §8 of REFACTORING.md). The
// Evidence-specific fields here capture only the quality/source
// triple; lattice / retention / lifecycle mechanics live on
// Belief / Task / Episode respectively.
//
// P0 ENTITY MODEL REFACTOR (item #4) — lands after Fact (item #3) and
// before Episode (item #5, which absorbs ConversationID / MessageID /
// ExtractedFrom). Pattern matches: minimal surface, identity via
// embedded Fact, quality triple as the explicit struct fields.
// The AsEvidence() down-direction projection (Entity → slim type) is
// the canonical API; the inverse Evidence.AsEntity() (lossy on
// embedded-Fact identity) was removed in §8.4.
type Evidence struct {
	Fact
	Confidence float32 `json:"confidence,omitempty"`
	Source     string  `json:"source,omitempty"`
	SourceType string  `json:"source_type,omitempty"`
}

// AsEvidence pulls the 3 evidence fields off an Entity and discards
// the remaining 16 (Status / ValidFrom..To / CreatedAt / UpdatedAt /
// LastAccessedAt / Archived / Degree / Priority / ID / Category /
// Content / Embedding / ConversationID / MessageID / ExtractedFrom).
// Callers that need the full row continue to use Entity.
func (e Entity) AsEvidence() Evidence {
	return Evidence{
		Confidence: e.Confidence,
		Source:     e.Source,
		SourceType: e.SourceType,
	}
}
