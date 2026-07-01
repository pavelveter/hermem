package evolution

import (
	"context"
	"database/sql"
	"math"
	"testing"

	"github.com/pavelveter/hermem/src/internal/memory/belief"
	"github.com/pavelveter/hermem/src/internal/memory/evidence"
	"github.com/pavelveter/hermem/src/internal/store"
)

// evidenceListAdapter wraps an evidence.Service so it satisfies EvidenceLister.
// This adapter lives in the test file because only tests need to bridge
// between the persistence layer ([]*evidence.Evidence) and the domain
// interface ([]EvidenceItem).
type evidenceListAdapter struct {
	svc evidence.Service
}

func (a *evidenceListAdapter) ListForBelief(ctx context.Context, beliefID int64) ([]EvidenceItem, error) {
	raw, err := a.svc.ListForBelief(ctx, beliefID)
	if err != nil {
		return nil, err
	}
	out := make([]EvidenceItem, len(raw))
	for i, e := range raw {
		out[i] = e
	}
	return out, nil
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("MemDBRandom: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPropagateConfidence_AllSupport(t *testing.T) {
	db := openDB(t)
	ctx := t.Context()
	bSvc := belief.New(db)
	eSvc := &evidenceListAdapter{svc: evidence.New(db)}
	b := &belief.Belief{Content: "test", Confidence: 1.0}
	if err := bSvc.CreateBelief(ctx, b); err != nil {
		t.Fatalf("CreateBelief: %v", err)
	}
	rawSvc := evidence.New(db)
	for _, e := range []*evidence.Evidence{
		{BeliefID: b.ID, Polarity: evidence.PolaritySupport, Strength: 0.8, Content: "sup1"},
		{BeliefID: b.ID, Polarity: evidence.PolaritySupport, Strength: 0.6, Content: "sup2"},
	} {
		if err := rawSvc.CreateEvidence(ctx, e); err != nil {
			t.Fatalf("CreateEvidence: %v", err)
		}
	}
	conf, err := PropagateConfidence(ctx, bSvc, eSvc, b.ID)
	if err != nil {
		t.Fatalf("PropagateConfidence: %v", err)
	}
	// support 0.8+0.6=1.4 / total 1.4 = 1.0
	if roundTo(conf, 4) != 1.0 {
		t.Errorf("expected 1.0, got %f", conf)
	}
}

func TestPropagateConfidence_MixedEvidence(t *testing.T) {
	db := openDB(t)
	ctx := t.Context()
	bSvc := belief.New(db)
	eSvc := &evidenceListAdapter{svc: evidence.New(db)}
	b := &belief.Belief{Content: "mixed", Confidence: 1.0}
	if err := bSvc.CreateBelief(ctx, b); err != nil {
		t.Fatalf("CreateBelief: %v", err)
	}
	rawSvc := evidence.New(db)
	for _, e := range []*evidence.Evidence{
		{BeliefID: b.ID, Polarity: evidence.PolaritySupport, Strength: 0.8, Content: "sup"},
		{BeliefID: b.ID, Polarity: evidence.PolarityRefute, Strength: 0.2, Content: "ref"},
	} {
		if err := rawSvc.CreateEvidence(ctx, e); err != nil {
			t.Fatalf("CreateEvidence: %v", err)
		}
	}
	conf, err := PropagateConfidence(ctx, bSvc, eSvc, b.ID)
	if err != nil {
		t.Fatalf("PropagateConfidence: %v", err)
	}
	expected := 0.8 / 1.0 // 0.8
	if roundTo(conf, 4) != roundTo(expected, 4) {
		t.Errorf("expected %f, got %f", expected, conf)
	}
}

func TestPropagateConfidence_NoEvidenceKeepsConfidence(t *testing.T) {
	db := openDB(t)
	ctx := t.Context()
	bSvc := belief.New(db)
	eSvc := &evidenceListAdapter{svc: evidence.New(db)}
	b := &belief.Belief{Content: "no-evic", Confidence: 0.7}
	if err := bSvc.CreateBelief(ctx, b); err != nil {
		t.Fatalf("CreateBelief: %v", err)
	}
	conf, err := PropagateConfidence(ctx, bSvc, eSvc, b.ID)
	if err != nil {
		t.Fatalf("PropagateConfidence: %v", err)
	}
	if math.Abs(conf-0.7) > 0.001 {
		t.Errorf("expected confidence unchanged ~0.7, got %f", conf)
	}
}

func TestPropagateConfidence_InvalidID(t *testing.T) {
	_, err := PropagateConfidence(t.Context(), nil, nil, 0)
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}
