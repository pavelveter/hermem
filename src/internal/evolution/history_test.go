package evolution

import (
	"testing"
)

func TestRecordAndListHistory(t *testing.T) {
	db := openDB(t)
	ctx := t.Context()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence) VALUES (1, 'test', 1.0)`); err != nil {
		t.Fatalf("insert belief: %v", err)
	}

	if err := RecordHistory(ctx, db, 1, 1.0, "Active", "initial"); err != nil {
		t.Fatalf("RecordHistory: %v", err)
	}
	if err := RecordHistory(ctx, db, 1, 0.7, "Active", "confidence propagation"); err != nil {
		t.Fatalf("RecordHistory: %v", err)
	}

	entries, err := ListHistory(ctx, db, 1)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Confidence != 1.0 {
		t.Errorf("expected first entry confidence=1.0, got %f", entries[0].Confidence)
	}
	if entries[1].Confidence != 0.7 {
		t.Errorf("expected second entry confidence=0.7, got %f", entries[1].Confidence)
	}
	if entries[1].Reason != "confidence propagation" {
		t.Errorf("expected reason 'confidence propagation', got %q", entries[1].Reason)
	}
}

func TestRecordHistory_InvalidID(t *testing.T) {
	err := RecordHistory(t.Context(), nil, 0, 1.0, "Active", "test")
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestListHistory_Empty(t *testing.T) {
	db := openDB(t)
	ctx := t.Context()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence) VALUES (1, 'test', 1.0)`); err != nil {
		t.Fatalf("insert belief: %v", err)
	}

	entries, err := ListHistory(ctx, db, 1)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty history, got %d entries", len(entries))
	}
}
