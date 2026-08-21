// Package domain extraction values are typed LLM extraction outputs and
// provenance.
package domain

// Relation is a typed connection between entities as extracted from dialog.
// The TargetID is treated as an opaque reference — ADR-035 governs how
// opaque references resolve to persistent entity IDs.
type Relation struct {
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

// ExtractedEntity is one entity extracted from a dialog by an LLM.
// The ID is the LLM-suggested entity identifier used in the ExtractionResult
// envelope. Public SPI surfaces (spi.ExtractResponse, domain.EntityDraft)
// remain identity-free; this is the LEGACY shape that internal ingestion
// and compression pipelines still consume.
//
// JSON tags are preserved so wire-storage formats (the pending.jsonl drain,
// persisted extraction logs) remain byte-compatible across the
// compatibility release and the breaking removal.
type ExtractedEntity struct {
	ID        string     `json:"id"`
	Category  string     `json:"category"`
	Content   string     `json:"content"`
	Relations []Relation `json:"relations"`
}

// ExtractionResult is the full output of an LLM extraction call.
// Mirrors the prompt's JSON envelope verbatim so the legacy extractors
// (which unmarshal the LLM response directly) round-trip without
// per-provider reshape layers.
type ExtractionResult struct {
	Entities []ExtractedEntity `json:"entities"`
}

// Provenance records where an ingested entity came from. Used by the
// ingestion pipeline to attach conversation/message provenance to fresh
// entity rows and to drive the contradiction handler's source merge
// policy. ConversationID and MessageID round-trip through
// the MemoryMessage envelope; ExtractedFrom carries the raw dialog so
// downstream explanations can quote the originating text.
type Provenance struct {
	ConversationID string
	MessageID      string
	ExtractedFrom  string
}

// LegacyEntityDraft converts an ExtractedEntity into the public,
// identity-free EntityDraft. The LLM-suggested ID is dropped during the
// conversion — ADR-035 governs whether the UUIDv7/content-addressed
// replacement is wired up in a follow-on change.
//
// Relation targets remain opaque references in the public draft — they
// are NOT promoted to persistent IDs at this layer.
func LegacyEntityDraft(entity ExtractedEntity) EntityDraft {
	relations := make([]RelationDraft, 0, len(entity.Relations))
	for _, relation := range entity.Relations {
		relations = append(relations, RelationDraft{
			TargetRef:    relation.TargetID,
			RelationType: relation.RelationType,
		})
	}
	return EntityDraft{
		Category:  entity.Category,
		Content:   entity.Content,
		Relations: relations,
	}
}

// LegacyExtractionDrafts converts a legacy ExtractionResult while dropping
// model-selected entity IDs from the public draft values.
func LegacyExtractionDrafts(result *ExtractionResult) []EntityDraft {
	if result == nil {
		return nil
	}
	drafts := make([]EntityDraft, 0, len(result.Entities))
	for _, entity := range result.Entities {
		drafts = append(drafts, LegacyEntityDraft(entity))
	}
	return drafts
}
