package ingestion

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// contradictionAction describes the outcome of handleContradiction.
type contradictionAction int

const (
	contradictionNone           contradictionAction = iota
	contradictionKeepBoth                           // HIGH-CONF: keep both entities
	contradictionPreferIncoming                     // LOW-CONF: archive existing, keep incoming
)

// handleContradiction detects contradictions between existing and incoming entities
// and returns the action to take, an optional archive ID, and any vector index ops.
func (w *IngestionWorker) handleContradiction(existing *core.Entity, incoming core.ExtractedEntity) (contradictionAction, string, []viOp) {
	if !w.detector.Detect(*existing, core.Entity{Content: incoming.Content}).Detected {
		return contradictionNone, "", nil
	}

	existingConf := existing.Confidence
	if existingConf == 0 {
		existingConf = 1.0
	}

	if existingConf >= 0.7 {
		slog.Debug("contradiction detected, keeping both", "existing_id", existing.ID, "incoming_id", incoming.ID)
		return contradictionKeepBoth, "", nil
	}

	slog.Debug("contradiction resolved: preferring incoming", "existing_id", existing.ID, "incoming_id", incoming.ID)
	return contradictionPreferIncoming, existing.ID, []viOp{{kind: viOpRemove, id: existing.ID}}
}

// mergeExistingEntity merges the incoming entity into the existing one and returns
// the merged entity with a re-embedded vector.
func (w *IngestionWorker) mergeExistingEntity(ctx context.Context, existing *core.Entity, incoming core.ExtractedEntity, prov core.Provenance) (*core.Entity, error) {
	mergedContent := existing.Content
	if !strings.Contains(existing.Content, incoming.Content) {
		mergedContent = existing.Content + "; " + incoming.Content
	}
	updatedEmb, err := w.embedder.Embed(ctx, mergedContent)
	if err != nil {
		return nil, err
	}
	vector.NormalizeVector(updatedEmb)
	return &core.Entity{
		ID:             existing.ID,
		Category:       existing.Category,
		Content:        mergedContent,
		Embedding:      updatedEmb,
		Status:         existing.Status,
		CreatedAt:      existing.CreatedAt,
		Confidence:     1.0,
		ConversationID: prov.ConversationID,
		MessageID:      prov.MessageID,
		ExtractedFrom:  prov.ExtractedFrom,
		Source:         "dialog",
		SourceType:     "extraction",
		UpdatedAt:      core.TimePtr(time.Now().UTC()),
	}, nil
}
