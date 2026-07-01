package hermem

import (
	"context"
	"net/http"
	"testing"
)

func TestMemoryStore(t *testing.T) {
	runSDKCall(t, "POST", "/store", http.StatusNoContent, nil, func(c *Client) {
		err := c.Memory.Store(context.Background(), &StoreRequest{
			ID: "paris", Category: "world", Content: "Paris is the capital of France",
		})
		if err != nil {
			t.Fatalf("Store: %v", err)
		}
	})
}

func TestMemorySearch(t *testing.T) {
	runSDKCall(t, "POST", "/search", http.StatusOK, []SearchResult{
		{Entity: Entity{ID: "paris", Category: "world", Content: "Paris is the capital of France"}, Similarity: 0.95},
	}, func(c *Client) {
		got, err := c.Memory.Search(context.Background(), &SearchRequest{Query: "capital", TopK: 5})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d results, want 1", len(got))
		}
		if got[0].Entity.ID != "paris" {
			t.Errorf("got id %q, want paris", got[0].Entity.ID)
		}
		if got[0].Similarity < 0.9 {
			t.Errorf("got similarity %f, want >= 0.9", got[0].Similarity)
		}
	})
}

func TestMemoryRetrieve(t *testing.T) {
	runSDKCall(t, "POST", "/retrieve", http.StatusOK, RetrievalResult{
		SeedNodes: []GraphNode{{Entity: Entity{ID: "paris", Category: "world", Content: "x"}}},
	}, func(c *Client) {
		got, err := c.Memory.Retrieve(context.Background(), &RetrieveRequest{SeedIDs: []string{"paris"}})
		if err != nil {
			t.Fatalf("Retrieve: %v", err)
		}
		if len(got.SeedNodes) != 1 {
			t.Fatalf("got %d seed nodes, want 1", len(got.SeedNodes))
		}
		if got.SeedNodes[0].Entity.ID != "paris" {
			t.Errorf("got id %q, want paris", got.SeedNodes[0].Entity.ID)
		}
	})
}

func TestMemoryQuery(t *testing.T) {
	runSDKCall(t, "POST", "/query", http.StatusOK, QueryResponse{Context: "Paris is the capital of France."}, func(c *Client) {
		got, err := c.Memory.Query(context.Background(), &SearchRequest{Query: "capital", TopK: 5})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if got.Context == "" {
			t.Fatal("got empty context")
		}
	})
}

func TestMemoryExplain(t *testing.T) {
	runSDKCall(t, "POST", "/query/explain", http.StatusOK, RetrievalResult{
		SeedNodes: []GraphNode{{Entity: Entity{ID: "paris", Category: "world", Content: "x"}}},
	}, func(c *Client) {
		got, err := c.Memory.Explain(context.Background(), &SearchRequest{Query: "capital", TopK: 5})
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		if len(got.SeedNodes) != 1 {
			t.Fatalf("got %d seed nodes, want 1", len(got.SeedNodes))
		}
	})
}

func TestMemoryIngest(t *testing.T) {
	runSDKCall(t, "POST", "/ingest", http.StatusNoContent, nil, func(c *Client) {
		err := c.Memory.Ingest(context.Background(), &IngestRequest{Dialog: "I love Paris."})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
	})
}

func TestMemoryEdge(t *testing.T) {
	runSDKCall(t, "POST", "/edge", http.StatusNoContent, nil, func(c *Client) {
		err := c.Memory.Edge(context.Background(), &EdgeRequest{
			SourceID: "a", TargetID: "b", RelationType: "knows", AutoCreate: true,
		})
		if err != nil {
			t.Fatalf("Edge: %v", err)
		}
	})
}

func TestMemoryReEmbed(t *testing.T) {
	runSDKCall(t, "POST", "/admin/re-embed", http.StatusOK, ReEmbedResult{
		TotalEntities: 100, ReEmbedded: 100, Skipped: 0, Failed: 0,
		Elapsed: "1.0s", OldDim: 384, NewDim: 384, Batches: 1,
	}, func(c *Client) {
		got, err := c.Memory.ReEmbed(context.Background(), &ReEmbedRequest{Dim: 384})
		if err != nil {
			t.Fatalf("ReEmbed: %v", err)
		}
		if got.TotalEntities != 100 {
			t.Errorf("got %d, want 100", got.TotalEntities)
		}
		if got.OldDim != 384 || got.NewDim != 384 {
			t.Errorf("dim: got %d -> %d, want 384 -> 384", got.OldDim, got.NewDim)
		}
	})
}

// TestMemoryError verifies that an HTTP 4xx response with a structured
// error envelope is propagated to the caller as *APIError with the
// correct status code, message, and code fields.
func TestMemoryError(t *testing.T) {
	runSDKCall(t, "POST", "/store", http.StatusBadRequest, `{"error":"validation failed","code":"invalid"}`, func(c *Client) {
		err := c.Memory.Store(context.Background(), &StoreRequest{ID: "x"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		apiErr, ok := err.(*APIError)
		if !ok {
			t.Fatalf("got %T, want *APIError", err)
		}
		if apiErr.StatusCode != 400 {
			t.Errorf("status: got %d, want 400", apiErr.StatusCode)
		}
		if apiErr.Code != "invalid" {
			t.Errorf("code: got %q, want invalid", apiErr.Code)
		}
		if apiErr.Message != "validation failed" {
			t.Errorf("message: got %q, want %q", apiErr.Message, "validation failed")
		}
	})
}
