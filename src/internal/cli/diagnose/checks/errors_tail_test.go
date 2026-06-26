package checks

import (
	"testing"
)

func TestCheckErrorsTail_ReturnsNote(t *testing.T) {
	r := CheckErrorsTail()
	if r.Note == "" {
		t.Error("expected non-empty Note")
	}
	if r.Entries != nil {
		t.Errorf("expected nil Entries, got %v", r.Entries)
	}
}
