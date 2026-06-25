package core

// Evidence is the quality / origin meta-block attached to a Fact or an
// Entity. It captures the confidence assigned to the underlying claim,
// the source identifier (= where it came from), and the source type
// classifier (= what kind of source). It carries NO identity, content,
// or lifecycle fields — those live on Fact / Task / Episode respectively.
//
// P0 ENTITY MODEL REFACTOR (item #4) — lands after Fact (item #3) and
// before Episode (item #5, which absorbs ConversationID / MessageID /
// ExtractedFrom). The pattern is the same as Fact: minimal surface,
// Entity keeps the 16 metadata fields it already has, conversion
// methods project and round-trip cleanly for callers that prefer the
// per-domain-model API.
type Evidence struct {
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

// AsEntity lifts the 3 evidence fields back into an Entity. The 16
// non-evidence fields are zeroed / nil. Callers that need them back
// must merge from a domain-specific source (Fact for content,
// Episode for provenance, Task for lifecycle, etc.).
func (ev Evidence) AsEntity() Entity {
	return Entity{
		Confidence: ev.Confidence,
		Source:     ev.Source,
		SourceType: ev.SourceType,
	}
}
