package health_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/health"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newHealthFixture(t *testing.T) (*health.Service, *sql.DB) {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return health.New(db), db
}

// --- New ---

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := health.New(db)
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

// --- Health ---

func TestHealth_ReturnsOk(t *testing.T) {
	svc, _ := newHealthFixture(t)
	result := svc.Health()
	if result["status"] != "ok" {
		t.Fatalf("want status=ok, got %v", result)
	}
}

// --- Live ---

func TestLive_ReturnsOk(t *testing.T) {
	svc, _ := newHealthFixture(t)
	result := svc.Live()
	if result["status"] != "ok" {
		t.Fatalf("want status=ok, got %v", result)
	}
}

// --- Ready ---

func TestReady_OK(t *testing.T) {
	svc, _ := newHealthFixture(t)
	code, result := svc.Ready(context.Background())
	if code != 200 {
		t.Fatalf("want 200, got %d: %v", code, result)
	}
	if result["status"] != "ok" {
		t.Fatalf("want status=ok, got %v", result)
	}
}

func TestReady_DBError(t *testing.T) {
	svc, db := newHealthFixture(t)
	db.Close() // force PingContext failure
	code, result := svc.Ready(context.Background())
	if code != 503 {
		t.Fatalf("want 503, got %d: %v", code, result)
	}
	if result["status"] != "degraded" {
		t.Fatalf("want status=degraded, got %v", result)
	}
}
