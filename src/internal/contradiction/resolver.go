package contradiction

import "github.com/pavelveter/hermem/src/internal/core"

// ResolutionAction describes the outcome of contradiction resolution.
type ResolutionAction int

const (
	// ActionKeepBoth means both entities coexist (high-confidence existing).
	ActionKeepBoth ResolutionAction = iota
	// ActionPreferIncoming means the existing entity is archived.
	ActionPreferIncoming
)

// ContradictionResolver decides what to do when an incoming entity
// contradicts an existing one. Implementations must be pure — they
// return a decision without side effects. The ingest pipeline applies
// the decision (archive, create edge, etc.).
type ContradictionResolver interface {
	Resolve(existing core.Entity, incoming core.ExtractedEntity) ResolutionAction
}

// ThresholdResolver implements ContradictionResolver using a
// confidence threshold. Existing entities with confidence >= threshold
// are kept alongside the incoming one; below threshold they are archived.
type ThresholdResolver struct {
	// Threshold is the minimum confidence for keeping both entities.
	// Default 0.7 when zero-valued.
	Threshold float32
}

// Resolve implements ContradictionResolver.
func (r *ThresholdResolver) Resolve(existing core.Entity, _ core.ExtractedEntity) ResolutionAction {
	t := r.Threshold
	if t <= 0 {
		t = 0.7
	}
	conf := existing.Confidence
	if conf == 0 {
		conf = 1.0
	}
	if conf >= t {
		return ActionKeepBoth
	}
	return ActionPreferIncoming
}
