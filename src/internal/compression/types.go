package compression

import (
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

type SummaryNode struct {
	ID             string     `json:"id"`
	Content        string     `json:"content"`
	CompressedFrom []string   `json:"compressed_from"`
	CompressedAt   time.Time  `json:"compressed_at"`
	Confidence     float32    `json:"confidence"`
	Provenance     string     `json:"provenance"`
	Generation     int        `json:"generation"`
	ExtractorModel string     `json:"extractor_model,omitempty"`
	SupersededBy   string     `json:"superseded_by,omitempty"`
	RegeneratedAt  *time.Time `json:"regenerated_at,omitempty"`
}

func (n SummaryNode) AsEntity() core.Entity {
	return core.Entity{
		ID:        n.ID,
		Category:  "summary",
		Content:   n.Content,
		UpdatedAt: n.CompressedAt,
	}
}

func EntityAsSummaryNode(e core.Entity) SummaryNode {
	return SummaryNode{
		ID:         e.ID,
		Content:    e.Content,
		Confidence: e.Confidence,
	}
}
