package evolution

import (
	"context"
	"testing"
)

func TestGetSupportRefute(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence) VALUES (1, 'test', 1.0)`); err != nil {
		t.Fatalf("insert belief: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO evidence (belief_id, polarity, strength, content) VALUES (1, 'support', 0.8, 'sup')`); err != nil {
			t.Fatalf("insert support evidence: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO evidence (belief_id, polarity, strength, content) VALUES (1, 'refute', 0.3, 'ref')`); err != nil {
			t.Fatalf("insert refute evidence: %v", err)
		}
	}

	r, err := GetSupportRefute(ctx, db, 1)
	if err != nil {
		t.Fatalf("GetSupportRefute: %v", err)
	}
	if r.Support != 3 {
		t.Errorf("expected Support=3, got %d", r.Support)
	}
	if r.Refute != 2 {
		t.Errorf("expected Refute=2, got %d", r.Refute)
	}
	if r.Total != 5 {
		t.Errorf("expected Total=5, got %d", r.Total)
	}
	if r.SupportPct != 60 {
		t.Errorf("expected SupportPct=60, got %f", r.SupportPct)
	}
	if r.RefutePct != 40 {
		t.Errorf("expected RefutePct=40, got %f", r.RefutePct)
	}
}

func TestGetSupportRefute_NoEvidence(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence) VALUES (1, 'test', 1.0)`); err != nil {
		t.Fatalf("insert belief: %v", err)
	}

	r, err := GetSupportRefute(ctx, db, 1)
	if err != nil {
		t.Fatalf("GetSupportRefute: %v", err)
	}
	if r.Total != 0 {
		t.Errorf("expected Total=0, got %d", r.Total)
	}
}

func TestGetSupportRefute_InvalidID(t *testing.T) {
	_, err := GetSupportRefute(context.Background(), nil, 0)
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}
