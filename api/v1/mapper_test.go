package v1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pavelveter/hermem/pkg/domain"
)

func TestStoreRequestGoldenJSON(t *testing.T) {
	body, err := json.Marshal(StoreRequest{ID: "e1", Category: "world", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"id":"e1","category":"world","content":"hello"}`
	if string(body) != want {
		t.Fatalf("golden JSON mismatch: got %s want %s", body, want)
	}
}

func TestStoreRequestMapsToDomain(t *testing.T) {
	r := StoreRequest{ID: "e1", Category: "world", Content: "hello", Embedding: []float32{1, 2}}
	e := r.ToDomain()
	if e.ID != r.ID || e.Category != r.Category || e.Content != r.Content || len(e.Embedding) != 2 {
		t.Fatalf("unexpected domain entity: %#v", e)
	}
}

func TestSearchAndRetrieveDefaults(t *testing.T) {
	if got := (SearchRequest{Query: "q"}).Normalize().TopK; got != 5 {
		t.Fatalf("TopK default: got %d", got)
	}
	if got := (RetrieveRequest{SeedIDs: []string{"e1"}}).Normalize().MaxDepth; got != 2 {
		t.Fatalf("MaxDepth default: got %d", got)
	}
}

func TestEntityFromDomainOmitsEmptyOptionalFields(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	body, err := json.Marshal(EntityFromDomain(domain.Entity{ID: "e1", Category: "world", Content: "hello", CreatedAt: &now}))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"id":"e1","category":"world","content":"hello","created_at":"2026-08-20T00:00:00Z"}`
	if string(body) != want {
		t.Fatalf("golden JSON mismatch: got %s want %s", body, want)
	}
}
