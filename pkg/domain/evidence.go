package domain

// Evidence captures confidence and source metadata for a semantic claim.
type Evidence struct {
	Fact
	Confidence float32 `json:"confidence,omitempty"`
	Source     string  `json:"source,omitempty"`
	SourceType string  `json:"source_type,omitempty"`
}

// AsEvidence projects an Entity into its evidence fields.
func (e Entity) AsEvidence() Evidence {
	return Evidence{Confidence: e.Confidence, Source: e.Source, SourceType: e.SourceType}
}
