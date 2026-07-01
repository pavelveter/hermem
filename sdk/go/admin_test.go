package hermem

import (
	"context"
	"net/http"
	"testing"
)

func TestAdminMigrateStatus(t *testing.T) {
	runSDKCall(t, "GET", "/db/migrate", http.StatusOK, []MigStatus{
		{Name: "001_init", Applied: true, ChecksumMatch: true},
	}, func(c *Client) {
		got, err := c.Admin.MigrateStatus(context.Background())
		if err != nil {
			t.Fatalf("MigrateStatus: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d, want 1", len(got))
		}
		if got[0].Name != "001_init" {
			t.Errorf("name: got %q, want 001_init", got[0].Name)
		}
		if !got[0].Applied {
			t.Error("expected applied=true")
		}
	})
}

func TestAdminSchema(t *testing.T) {
	runSDKCall(t, "GET", "/db/schema", http.StatusOK, SchemaReport{
		Stored: "abc", Current: "abc", DriftDetected: false,
	}, func(c *Client) {
		got, err := c.Admin.Schema(context.Background())
		if err != nil {
			t.Fatalf("Schema: %v", err)
		}
		if got == nil {
			t.Fatal("got nil")
		}
		if got.Stored != got.Current {
			t.Errorf("drift expected: stored=%q current=%q", got.Stored, got.Current)
		}
		if got.DriftDetected {
			t.Error("expected drift_detected=false")
		}
	})
}

func TestAdminVerifyDB(t *testing.T) {
	runSDKCall(t, "GET", "/db/verify", http.StatusNoContent, nil, func(c *Client) {
		err := c.Admin.VerifyDB(context.Background())
		if err != nil {
			t.Fatalf("VerifyDB: %v", err)
		}
	})
}

func TestAdminRollback(t *testing.T) {
	runSDKCall(t, "POST", "/db/rollback", http.StatusNoContent, nil, func(c *Client) {
		err := c.Admin.Rollback(context.Background())
		if err != nil {
			t.Fatalf("Rollback: %v", err)
		}
	})
}

func TestAdminHealth(t *testing.T) {
	runSDKCall(t, "GET", "/health", http.StatusOK, HealthResponse{Status: "ok"}, func(c *Client) {
		got, err := c.Admin.Health(context.Background())
		if err != nil {
			t.Fatalf("Health: %v", err)
		}
		if got.Status != "ok" {
			t.Errorf("status: got %q, want ok", got.Status)
		}
	})
}

func TestAdminReady(t *testing.T) {
	runSDKCall(t, "GET", "/health/ready", http.StatusOK, ReadyResponse{
		Status: "ready", LatencyMs: 12, Checks: nil,
	}, func(c *Client) {
		got, err := c.Admin.Ready(context.Background())
		if err != nil {
			t.Fatalf("Ready: %v", err)
		}
		if got.Status != "ready" {
			t.Errorf("status: got %q, want ready", got.Status)
		}
		if got.LatencyMs != 12 {
			t.Errorf("latency: got %d, want 12", got.LatencyMs)
		}
	})
}

func TestAdminError(t *testing.T) {
	runSDKCall(t, "GET", "/db/verify", http.StatusServiceUnavailable, `{"error":"db unavailable","code":"db_down"}`, func(c *Client) {
		err := c.Admin.VerifyDB(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		apiErr, ok := err.(*APIError)
		if !ok {
			t.Fatalf("got %T, want *APIError", err)
		}
		if apiErr.StatusCode != 503 {
			t.Errorf("status: got %d, want 503", apiErr.StatusCode)
		}
		if apiErr.Code != "db_down" {
			t.Errorf("code: got %q, want db_down", apiErr.Code)
		}
	})
}
