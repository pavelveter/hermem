package hermem

import (
	"context"
	"net/http"
	"testing"
)

func TestGraphVerify(t *testing.T) {
	runSDKCall(t, "GET", "/graph/verify", http.StatusOK, VerifyReport{Issues: nil}, func(c *Client) {
		got, err := c.Graph.Verify(context.Background())
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if got == nil {
			t.Fatal("got nil")
		}
	})
}

func TestGraphContradictions(t *testing.T) {
	runSDKCall(t, "GET", "/contradictions", http.StatusOK, []ContradictionPair{
		{SourceID: "a", SourceContent: "x", TargetID: "b", TargetContent: "y"},
	}, func(c *Client) {
		got, err := c.Graph.Contradictions(context.Background(), "")
		if err != nil {
			t.Fatalf("Contradictions: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d, want 1", len(got))
		}
		if got[0].SourceID != "a" {
			t.Errorf("source_id: got %q, want a", got[0].SourceID)
		}
	})
}

// TestGraphContradictionsWithID verifies the SDK sends ?id=<encoded>
// when an entity id is supplied.
func TestGraphContradictionsWithID(t *testing.T) {
	runSDKCallWithQuery(t, "GET", "/contradictions", "id=paris", http.StatusOK, []ContradictionPair{}, func(c *Client) {
		got, err := c.Graph.Contradictions(context.Background(), "paris")
		if err != nil {
			t.Fatalf("Contradictions: %v", err)
		}
		if got == nil {
			t.Fatal("got nil")
		}
	})
}

func TestGraphConnectedComponents(t *testing.T) {
	runSDKCall(t, "GET", "/connected-components", http.StatusOK, []ConnectedComponent{
		{Size: 3, AvgDegree: 1.5},
	}, func(c *Client) {
		got, err := c.Graph.ConnectedComponents(context.Background(), 0) // 0 = no min_size query
		if err != nil {
			t.Fatalf("ConnectedComponents: %v", err)
		}
		if len(got) != 1 || got[0].Size != 3 {
			t.Errorf("got %+v, want one component of size 3", got)
		}
	})
}

// TestGraphConnectedComponentsWithMinSize verifies the SDK sends
// ?min_size=N when minSize > 0.
func TestGraphConnectedComponentsWithMinSize(t *testing.T) {
	runSDKCallWithQuery(t, "GET", "/connected-components", "min_size=2", http.StatusOK, []ConnectedComponent{}, func(c *Client) {
		_, err := c.Graph.ConnectedComponents(context.Background(), 2)
		if err != nil {
			t.Fatalf("ConnectedComponents: %v", err)
		}
	})
}

func TestGraphCommunities(t *testing.T) {
	runSDKCall(t, "GET", "/communities", http.StatusOK, map[string]interface{}{
		"communities":       []interface{}{},
		"global_modularity": 0.0,
		"total_communities": 0,
	}, func(c *Client) {
		got, err := c.Graph.Communities(context.Background(), 0, 0)
		if err != nil {
			t.Fatalf("Communities: %v", err)
		}
		if got == nil {
			t.Fatal("got nil")
		}
	})
}

func TestGraphTimeline(t *testing.T) {
	runSDKCall(t, "GET", "/timeline", http.StatusOK, []TimelineEntry{
		{ID: "t1", Category: "world", Content: "x", CreatedAt: "2024-01-01T00:00:00Z"},
	}, func(c *Client) {
		got, err := c.Graph.Timeline(context.Background(), 0) // 0 = no limit query
		if err != nil {
			t.Fatalf("Timeline: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d, want 1", len(got))
		}
	})
}

// TestGraphTimelineWithLimit verifies the SDK sends ?limit=N.
func TestGraphTimelineWithLimit(t *testing.T) {
	runSDKCallWithQuery(t, "GET", "/timeline", "limit=10", http.StatusOK, []TimelineEntry{}, func(c *Client) {
		_, err := c.Graph.Timeline(context.Background(), 10)
		if err != nil {
			t.Fatalf("Timeline: %v", err)
		}
	})
}

func TestGraphProvenance(t *testing.T) {
	runSDKCall(t, "GET", "/provenance", http.StatusOK, []Entity{
		{ID: "e1", Category: "world", Content: "x"},
	}, func(c *Client) {
		got, err := c.Graph.Provenance(context.Background(), "", "", "", 0)
		if err != nil {
			t.Fatalf("Provenance: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d, want 1", len(got))
		}
	})
}

func TestGraphRecoveryPlan(t *testing.T) {
	runSDKCallWithQuery(t, "GET", "/recovery-plan", "id=task-1", http.StatusOK, []Entity{
		{ID: "rb-1", Category: "task", Content: "recover"},
	}, func(c *Client) {
		got, err := c.Graph.RecoveryPlan(context.Background(), "task-1")
		if err != nil {
			t.Fatalf("RecoveryPlan: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d, want 1", len(got))
		}
	})
}

func TestGraphError(t *testing.T) {
	runSDKCall(t, "GET", "/graph/verify", http.StatusInternalServerError, `{"error":"db down"}`, func(c *Client) {
		_, err := c.Graph.Verify(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		apiErr, ok := err.(*APIError)
		if !ok {
			t.Fatalf("got %T, want *APIError", err)
		}
		if apiErr.StatusCode != 500 {
			t.Errorf("status: got %d, want 500", apiErr.StatusCode)
		}
	})
}
