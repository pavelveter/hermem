package core

// Compose reassembles a full Entity from the 5 per-domain model
// projections. This is the canonical path for producers needing a
// slim→Entity reassembly — the inverse X.AsEntity() bridges (Task,
// Goal, Episode, Evidence, Belief) that previously handled this
// were removed in §8.4 because they silently dropped the embedded
// Fact identity. The only remaining AsEntity() bridges are
// Fact.AsEntity and compression.SummaryNode.AsEntity, both of
// which preserve identity and remain the safe direct mappings.
// (P0 ENTITY MODEL REFACTOR item #9)
//
// Usage:
//
//	e := core.Compose(f.AsFact(), ev.AsEvidence(), ep.AsEpisode(),
//	                 tk.AsTask(), b.AsBelief())
//	// or:  e := core.Compose(fact, evidence, episode, task, belief)
//
// Why a FREE function instead of a method on Entity:
//   - All 5 inputs come from outside — there is no useful receiver.
//   - (e Entity).Compose(...) would have an unused receiver, which
//     is awkward Go style and confuses "mutates e" vs "returns a new
//     e" semantics for reviewers.
//   - Free functions also compose cleanly inside aggregations (e.g.
//     a list-comprehension style rebuild loop in retention.Service).
//
// Field-by-field assignment locks the canonical 19-field layout.
// Order: Fact → Evidence → Episode → Task → Belief. Callers who
// pass zero-value models get zero values for those bands — Compose
// does NOT panic on partial input.
//
// Goal (item #7) re-views Task's shape but does not participate in
// Compose directly (Compose's type list is fixed at 5). Callers
// assembling a Goal-typed Entity do an inline 4-field copy from
// Goal → Task — Status, ValidFrom, ValidTo, Priority — and pass the
// resulting Task into Compose. No Goal.AsTask() bridge method is
// provided; the inline-copy pattern is the contract.
func Compose(f Fact, ev Evidence, ep Episode, t Task, b Belief) Entity {
	return Entity{
		// Fact band.
		ID:        f.ID,
		Category:  f.Category,
		Content:   f.Content,
		Embedding: f.Embedding,
		// Evidence band.
		Confidence: ev.Confidence,
		Source:     ev.Source,
		SourceType: ev.SourceType,
		// Episode band.
		ConversationID: ep.ConversationID,
		MessageID:      ep.MessageID,
		ExtractedFrom:  ep.ExtractedFrom,
		// Task band. Goal re-views this shape via inline 4-field copy
		// (no Goal.AsTask() bridge — by design).
		Status:    t.Status,
		ValidFrom: t.ValidFrom,
		ValidTo:   t.ValidTo,
		Priority:  t.Priority,
		// Belief band.
		CreatedAt:      b.CreatedAt,
		UpdatedAt:      b.UpdatedAt,
		LastAccessedAt: b.LastAccessedAt,
		Archived:       b.Archived,
		Degree:         b.Degree,
	}
}
