package compression

import (
	"time"

	"github.com/pavelveter/hermem/pkg/domain"
)

// SummaryNode carries the compressed-blob meta-block for a Fact.
//
// §8 NOTE: not enriched with domain.Fact embed (unlike Task/Goal/Episode/Belief/Evidence).
// Reason: SummaryNode declares its own ID/Content fields, and Go's
// anonymous-field-promotion rule rejects the `SummaryNode{ID: "x"}` literal
// form because the promoted Fact.ID would shadow the outer one. Deferred
// to a follow-up (named embed + caller literal migration). Identity stays
// via direct ID/Content fields for now.
type SummaryNode struct {
	ID             string     `json:"id"`
	Content        string     `json:"content"`
	CompressedFrom []string   `json:"compressed_from"`
	CompressedAt   *time.Time `json:"compressed_at"`
	Confidence     float32    `json:"confidence"`
	Provenance     string     `json:"provenance"`
	Generation     int        `json:"generation"`
	ExtractorModel string     `json:"extractor_model,omitempty"`
	SupersededBy   string     `json:"superseded_by,omitempty"`
	RegeneratedAt  *time.Time `json:"regenerated_at,omitempty"`
}

func (n SummaryNode) AsEntity() domain.Entity {
	return domain.Entity{
		ID:        n.ID,
		Category:  "summary",
		Content:   n.Content,
		UpdatedAt: n.CompressedAt,
	}
}
