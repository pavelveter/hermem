package core

import "github.com/pavelveter/hermem/pkg/domain"

// Compose reassembles a full Entity from the 5 per-domain model
// projections (Fact → Evidence → Episode → Task → Belief). This is the
// canonical path for producers needing a slim→Entity reassembly.
//
// Deprecated: call domain.Compose directly.
func Compose(f Fact, ev Evidence, ep Episode, t Task, b Belief) Entity {
	return domain.Compose(f, ev, ep, t, b)
}
