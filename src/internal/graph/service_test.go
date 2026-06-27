package graph

import (
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestService_Components_EmptyDBReturnsEmpty(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	comps, err := svc.Components(t.Context(), 2)
	if err != nil {
		t.Fatalf("Components: %v", err)
	}
	if comps == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(comps) != 0 {
		t.Errorf("want 0 components on empty DB, got %d", len(comps))
	}
}

func TestService_Components_MinSizeZeroSweepsAll(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	// minSize <= 0 means store returns all components regardless of size.
	// Empty DB has zero components, so len == 0 matches the no-component
	// case; the test pins zero-result normalization rather than the
	// minSize semantics.
	comps, err := svc.Components(t.Context(), 0)
	if err != nil {
		t.Fatalf("Components: %v", err)
	}
	if comps == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(comps) != 0 {
		t.Errorf("want 0 components on empty DB, got %d", len(comps))
	}
}

func TestService_Communities_EmptyDBReturnsEmpty(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	comms, q, err := svc.Communities(t.Context(), 10)
	if err != nil {
		t.Fatalf("Communities: %v", err)
	}
	if comms == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(comms) != 0 {
		t.Errorf("want 0 communities on empty DB, got %d", len(comms))
	}
	if q != 0 {
		t.Errorf("want globalQ=0 on empty DB, got %v", q)
	}
}

func TestService_Verify_EmptyDBReturnsClean(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	report, err := svc.Verify(t.Context(), core.DefaultSchemaConfig(false), 3)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.Pass() {
		t.Errorf("want Pass()=true on empty DB; issues: %s", strings.Join(report.Issues, "; "))
	}
}
