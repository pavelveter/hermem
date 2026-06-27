package evolution

import (
	"database/sql"
	"math"
	"testing"

	"github.com/pavelveter/hermem/src/internal/memory/belief"
	"github.com/pavelveter/hermem/src/internal/memory/evidence"
	"github.com/pavelveter/hermem/src/internal/store"
)

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
	eSvc := evidence.New(db)
	b := &belief.Belief{Content: "test", Confidence: 1.0}
	if err := bSvc.CreateBelief(ctx, b); err != nil {
		t.Fatalf("CreateBelief: %v", err)
	}
	for _, e := range []*evidence.Evidence{
		{BeliefID: b.ID, Polarity: evidence.PolaritySupport, Strength: 0.8, Content: "sup1"},
		{BeliefID: b.ID, Polarity: evidence.PolaritySupport, Strength: 0.6, Content: "sup2"},
	} {
		if err := eSvc.CreateEvidence(ctx, e); err != nil {
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
	eSvc := evidence.New(db)
	b := &belief.Belief{Content: "mixed", Confidence: 1.0}
	if err := bSvc.CreateBelief(ctx, b); err != nil {
		t.Fatalf("CreateBelief: %v", err)
	}
	for _, e := range []*evidence.Evidence{
		{BeliefID: b.ID, Polarity: evidence.PolaritySupport, Strength: 0.8, Content: "sup"},
		{BeliefID: b.ID, Polarity: evidence.PolarityRefute, Strength: 0.2, Content: "ref"},
	} {
		if err := eSvc.CreateEvidence(ctx, e); err != nil {
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
	eSvc := evidence.New(db)
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
