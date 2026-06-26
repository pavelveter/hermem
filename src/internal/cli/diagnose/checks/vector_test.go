package checks

import (
	"testing"
)

func TestCheckVector_CleanDB(t *testing.T) {
	db := openDB(t)
	r, err := CheckVector(db, 3)
	if err != nil {
		t.Fatalf("CheckVector: %v", err)
	}
	if r.TotalRows == 0 {
		t.Log("expected TotalRows=0 on clean DB, got 0 (ok)")
	}
	if r.ConfigDim != 3 {
		t.Errorf("expected ConfigDim=3, got %d", r.ConfigDim)
	}
	if r.StoredDim != 3 {
		t.Errorf("expected StoredDim=3, got %d", r.StoredDim)
	}
	if r.DimMismatch {
		t.Error("expected DimMismatch=false")
	}
}

func TestCheckVector_DimMismatch(t *testing.T) {
	db := openDB(t)
	r, err := CheckVector(db, 768)
	if err != nil {
		t.Fatalf("CheckVector: %v", err)
	}
	if !r.DimMismatch {
		t.Error("expected DimMismatch=true when configured dim differs from stored dim")
	}
}
