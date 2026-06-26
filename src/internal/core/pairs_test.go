package core

import (
	"reflect"
	"testing"
	"time"
)

// modelKind is a discriminator over the 5 per-domain projection
// types. Goal mirrors Task for projection mechanics (item #7) and
// is exercised separately in TestGoalProjectionReducesToTask; it
// does not enter the cross-pair matrix.
type modelKind int

const (
	kindFact modelKind = iota
	kindEvidence
	kindEpisode
	kindTask
	kindBelief
)

func (k modelKind) name() string {
	switch k {
	case kindFact:
		return "Fact"
	case kindEvidence:
		return "Evidence"
	case kindEpisode:
		return "Episode"
	case kindTask:
		return "Task"
	case kindBelief:
		return "Belief"
	}
	return "?"
}

func allKinds() []modelKind {
	return []modelKind{kindFact, kindEvidence, kindEpisode, kindTask, kindBelief}
}

// projectFrom runs Entity → k (the per-domain projection). Returns
// the projection wrapped in any so callers can switch on the kind.
func (k modelKind) projectFrom(e Entity) any {
	switch k {
	case kindFact:
		return e.AsFact()
	case kindEvidence:
		return e.AsEvidence()
	case kindEpisode:
		return e.AsEpisode()
	case kindTask:
		return e.AsTask()
	case kindBelief:
		return e.AsBelief()
	}
	return nil
}

// restore runs k (typed via any) → Entity using Compose, threading
// the model into its band slot and zeroing the other 4 bands. This
// is the §8.4 replacement for the deleted X.AsEntity() bridges —
// the bridge variants were lossy on the embedded-Fact identity, so
// Compose is the only safe round-trip option now.
func (k modelKind) restore(v any) Entity {
	switch x := v.(type) {
	case Fact:
		return Compose(x, Evidence{}, Episode{}, Task{}, Belief{})
	case Evidence:
		return Compose(Fact{}, x, Episode{}, Task{}, Belief{})
	case Episode:
		return Compose(Fact{}, Evidence{}, x, Task{}, Belief{})
	case Task:
		return Compose(Fact{}, Evidence{}, Episode{}, x, Belief{})
	case Belief:
		return Compose(Fact{}, Evidence{}, Episode{}, Task{}, x)
	}
	return Entity{}
}

// makeFilledEntity constructs an Entity with every one of the 19
// fields set to a distinct, non-zero sentinel value where possible.
// String fields get a per-band sentinel-character-suffix
// ("F-" / "E-" / "Ep-" / "T-" — for Fact / Evidence / Episode /
// Task) so any projection bleed-through is visible in failing-test
// output. Belief has no string fields, so its sentinels are
// numeric / bool / timestamp (Degree=42, Archived=true,
// UpdatedAt=now, etc.) — no string suffix is possible.
func makeFilledEntity() Entity {
	now := time.Now()
	later := now.Add(24 * time.Hour)
	earlier := now.Add(-7 * 24 * time.Hour)
	return Entity{
		// Fact band.
		ID:        "id-F",
		Category:  "world-F",
		Content:   "content-F",
		Embedding: []float32{0.1, 0.2, 0.3},
		// Evidence band.
		Confidence: 0.99,
		Source:     "source-E",
		SourceType: "type-E",
		// Episode band.
		ConversationID: "conv-Ep",
		MessageID:      "msg-Ep",
		ExtractedFrom:  "ext-Ep",
		// Task band.
		Status:    "active-T",
		ValidFrom: &earlier,
		ValidTo:   &later,
		Priority:  7,
		// Belief band.
		CreatedAt:      &earlier,
		UpdatedAt:      &now,
		LastAccessedAt: &later,
		Archived:       true,
		Degree:         42,
	}
}

// zeroY returns the structural zero-value for kind yk as an any,
// bypassing the projection machinery entirely. Using a struct
// literal (not `yk.projectFrom(Entity{})`) keeps the cross-pair
// "want" genuinely zero — the test then catches any projection
// that returns spurious non-zero values from a zero-band Entity.
func zeroY(yk modelKind) any {
	switch yk {
	case kindFact:
		return Fact{}
	case kindEvidence:
		return Evidence{}
	case kindEpisode:
		return Episode{}
	case kindTask:
		return Task{}
	case kindBelief:
		return Belief{}
	}
	return nil
}

// TestCrossPairMatrix_BandProjectionOrthogonality locks the
// pairwise orthogonal-band property for every ordered pair (X, Y)
// where X, Y ∈ {Fact, Evidence, Episode, Task, Belief}. Total:
// 25 subtests in a 5×5 matrix (5 self-pairs + 20 cross-pairs).
//
// Per pair (X, Y), the invariant is:
//
//	mid := entity → X → Entity
//	mid has ONLY X-band values; the other 4 bands are zeroed.
//
//	Then mid → Y:
//	- Self-pair (X == Y): mid → Y is the same as entity → Y,
//	  because round-tripping the X-band preserved its values.
//	- Cross-pair (X != Y): mid → Y is zero, because the Y-band of
//	  mid was zeroed by the X-restoration step. Equivalently:
//	  mid.AsY() == yk.projectFrom(Entity{}) for all X ≠ Y.
//
// Together these two cases prove the projection methods are
// pairwise-orthogonal — no band pollutes or is polluted by
// another band's projection, regardless of which X we route
// through.
func TestCrossPairMatrix_BandProjectionOrthogonality(t *testing.T) {
	entity := makeFilledEntity()

	for _, xk := range allKinds() {
		for _, yk := range allKinds() {
			xk2, yk2 := xk, yk
			name := xk2.name() + "_to_" + yk2.name()
			t.Run(name, func(t *testing.T) {
				mid := xk2.restore(xk2.projectFrom(entity))

				var want any
				if xk2 == yk2 {
					want = yk2.projectFrom(entity) // self-pair: round-trip preserves
					// Meta-check (fatal on purpose): if yk.projectFrom(entity)
					// equals the structural zero, AsY() is broken-to-zero and
					// the self-pair assertion below is meaningless (always-pass).
					// We use t.Fatalf here so the assertion below — which would
					// otherwise emit a noisy drift diff — is skipped.
					if reflect.DeepEqual(want, zeroY(yk2)) {
						t.Fatalf("%s meta-check FAIL: yk.projectFrom(filled entity) == zeroY — projection is broken", name)
					}
				} else {
					want = zeroY(yk2) // cross-pair: Y is zero in mid
				}
				got := yk2.projectFrom(mid)

				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s Y-projection drift:\n  want %+v\n  got  %+v",
						name, want, got)
				}
			})
		}
	}
}

// TestCrossPairMatrix_PointerIdentityOnTimeFields locks
// *time.Time pointer semantics across the X → Entity → Y chain
// for every ordered pair (X, Y) where Y has *time.Time fields
// (Task: ValidFrom / ValidTo, Belief: CreatedAt / LastAccessedAt).
// Total: 10 subtests — 1 self-pair + 4 cross-pairs, per time-
// bearing Y.
//
// Per pair (X, Y):
//   - Self-pair (X = Y ∈ {Task, Belief}): Y's *time.Time fields
//     are pointer-identical to the original Entity's Y-band
//     pointers (round-trip preserves).
//   - Cross-pair (X ≠ Y where Y has *time.Time): Y's *time.Time
//     fields are nil — mid's Y-band has been zeroed by the
//     X-restoration, so cross-pair round-tripping loses the
//     pointer reference. (This is the expected behavior: only
//     the Y-band itself survives an X-projection when X = Y.)
func TestCrossPairMatrix_PointerIdentityOnTimeFields(t *testing.T) {
	entity := makeFilledEntity()
	timeKinds := []modelKind{kindTask, kindBelief}

	for _, yk := range timeKinds {
		// Self-pair (X = Y). yMid <-> yk projected from entity.
		yk2 := yk
		t.Run(yk2.name()+"_self", func(t *testing.T) {
			mid := yk2.restore(yk2.projectFrom(entity))
			got := yk2.projectFrom(mid)
			switch yk2 {
			case kindTask:
				assertTaskPointersIdentical(t, yk2.name()+"_self", entity, got.(Task)) //nolint:errcheck // switch on yk2 above guarantees kindTask; assertTaskPointers... handles nil pointers
			case kindBelief:
				assertBeliefPointersIdentical(t, yk2.name()+"_self", entity, got.(Belief))
			}
		})

		// Cross-pairs (X ≠ Y). yMid mid's Y-band is zero (zeroed by
		// X-restoration), so the *time.Time pointers must be nil.
		for _, xk := range allKinds() {
			if xk == yk {
				continue
			}
			xk3, yk3 := xk, yk
			name := xk3.name() + "_to_" + yk3.name()
			t.Run(name, func(t *testing.T) {
				mid := xk3.restore(xk3.projectFrom(entity))
				got := yk3.projectFrom(mid)
				switch yk3 {
				case kindTask:
					assertTaskPointersNil(t, name, got.(Task))
				case kindBelief:
					assertBeliefPointersNil(t, name, got.(Belief))
				}
			})
		}
	}
}

// assertTaskPointersIdentical locks pointer-identity preservation
// for Task's two validity-window fields after a self-pair route.
func assertTaskPointersIdentical(t *testing.T, name string, original Entity, ty Task) {
	t.Helper()
	if ty.ValidFrom != original.ValidFrom {
		t.Errorf("%s Task.ValidFrom pointer drift: want %p, got %p",
			name, original.ValidFrom, ty.ValidFrom)
	}
	if ty.ValidTo != original.ValidTo {
		t.Errorf("%s Task.ValidTo pointer drift: want %p, got %p",
			name, original.ValidTo, ty.ValidTo)
	}
}

// assertTaskPointersNil locks that Task's *time.Time window-pointers
// are nil after a cross-pair route (mid's Y-band has been zeroed).
func assertTaskPointersNil(t *testing.T, name string, ty Task) {
	t.Helper()
	if ty.ValidFrom != nil {
		t.Errorf("%s Task.ValidFrom not nil after cross-pair round-trip: %p",
			name, ty.ValidFrom)
	}
	if ty.ValidTo != nil {
		t.Errorf("%s Task.ValidTo not nil after cross-pair round-trip: %p",
			name, ty.ValidTo)
	}
}

// assertBeliefPointersIdentical locks pointer-identity preservation
// for Belief's two timestamp fields after a self-pair route.
func assertBeliefPointersIdentical(t *testing.T, name string, original Entity, bv Belief) {
	t.Helper()
	if bv.CreatedAt != original.CreatedAt {
		t.Errorf("%s Belief.CreatedAt pointer drift: want %p, got %p",
			name, original.CreatedAt, bv.CreatedAt)
	}
	if bv.LastAccessedAt != original.LastAccessedAt {
		t.Errorf("%s Belief.LastAccessedAt pointer drift: want %p, got %p",
			name, original.LastAccessedAt, bv.LastAccessedAt)
	}
}

// assertBeliefPointersNil locks that Belief's *time.Time timestamp-
// pointers are nil after a cross-pair route (mid's Y-band has been
// zeroed).
func assertBeliefPointersNil(t *testing.T, name string, bv Belief) {
	t.Helper()
	if bv.CreatedAt != nil {
		t.Errorf("%s Belief.CreatedAt not nil after cross-pair round-trip: %p",
			name, bv.CreatedAt)
	}
	if bv.LastAccessedAt != nil {
		t.Errorf("%s Belief.LastAccessedAt not nil after cross-pair round-trip: %p",
			name, bv.LastAccessedAt)
	}
}
