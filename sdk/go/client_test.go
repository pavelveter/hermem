package hermem

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	c := New("http://localhost:8420")
	if c.baseURL != "http://localhost:8420" {
		t.Fatalf("expected baseURL http://localhost:8420, got %s", c.baseURL)
	}
	if c.Memory == nil {
		t.Fatal("Memory client is nil")
	}
	if c.Task == nil {
		t.Fatal("Task client is nil")
	}
	if c.Graph == nil {
		t.Fatal("Graph client is nil")
	}
	if c.Admin == nil {
		t.Fatal("Admin client is nil")
	}
}

func TestNewClientWithAPIKey(t *testing.T) {
	c := New("http://localhost:8420", WithAPIKey("test-key"))
	if c.apiKey != "test-key" {
		t.Fatalf("expected apiKey test-key, got %s", c.apiKey)
	}
}

func TestNewClientWithTimeout(t *testing.T) {
	c := New("http://localhost:8420", WithTimeout(5000))
	if c.httpClient.Timeout != 5000 {
		t.Fatalf("expected timeout 5000, got %v", c.httpClient.Timeout)
	}
}

func TestAPIError(t *testing.T) {
	e := &APIError{
		StatusCode: 404,
		Message:    "not found",
		Code:       "not_found",
	}
	if e.Error() != "hermem: not found (code=not_found, status=404)" {
		t.Fatalf("unexpected error string: %s", e.Error())
	}
}

func TestAPIErrorNoCode(t *testing.T) {
	e := &APIError{
		StatusCode: 500,
		Message:    "internal error",
	}
	if e.Error() != "hermem: internal error (status=500)" {
		t.Fatalf("unexpected error string: %s", e.Error())
	}
}
