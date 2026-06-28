package helpers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// HTTPClient wraps http.Client with helper methods for testing.
type HTTPClient struct {
	BaseURL string
	Client  *http.Client
	APIKey  string
}

// NewHTTPClient creates a client pointing at the given server.
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// WithAuth sets the API key for authenticated requests.
func (c *HTTPClient) WithAuth(key string) *HTTPClient {
	c.APIKey = key
	return c
}

// Do sends a request and returns the response.
func (c *HTTPClient) Do(t *testing.T, method, path string, body interface{}) *http.Response {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}

	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	return resp
}

// Get sends a GET request.
func (c *HTTPClient) Get(t *testing.T, path string) *http.Response {
	t.Helper()
	return c.Do(t, http.MethodGet, path, nil)
}

// Post sends a POST request with JSON body.
func (c *HTTPClient) Post(t *testing.T, path string, body interface{}) *http.Response {
	t.Helper()
	return c.Do(t, http.MethodPost, path, body)
}

// MustStatus asserts the response has the expected status code.
func MustStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected status %d, got %d: %s", expected, resp.StatusCode, string(body))
	}
}

// MustJSON decodes the response body into the target.
func MustJSON(t *testing.T, resp *http.Response, target interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// MustJSONMap decodes the response body into a map.
func MustJSONMap(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	MustJSON(t, resp, &m)
	return m
}

// BodyString reads and returns the response body as a string.
func BodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
