// Package hermem provides an official Go client for the Hermem API.
//
// Usage:
//
//	client := hermem.New("http://localhost:8420")
//	client.WithAPIKey("your-api-key")
//
//	// Store an entity
//	err := client.Memory.Store(ctx, &hermem.StoreRequest{
//		ID:       "paris",
//		Category: "world",
//		Content:  "Paris is the capital of France",
//	})
//
//	// Search
//	results, err := client.Memory.Search(ctx, &hermem.SearchRequest{
//		Query: "capital of France",
//		TopK:  5,
//	})
package hermem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SDKVersion is the Go SDK's semantic version. Must match the server's
// MAJOR version for compatibility.
const SDKVersion = "0.1.0"

// Client is the Hermem API client.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	Memory *MemoryClient
	Task   *TaskClient
	Graph  *GraphClient
	Admin  *AdminClient

	// OnVersionMismatch is called (at most once) when the server's
	// MAJOR version differs from the SDK's MAJOR version. The
	// arguments are (serverVersion, sdkVersion). If nil, version
	// mismatches are silently ignored.
	OnVersionMismatch func(server, sdk string)

	versionCheckOnce sync.Once
}

// Option configures the client.
type Option func(*Client)

// WithAPIKey sets the API key for authentication.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithTimeout sets a per-request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient = &http.Client{Timeout: d}
	}
}

// New returns a new Hermem client targeting the given base URL.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.Memory = &MemoryClient{c: c}
	c.Task = &TaskClient{c: c}
	c.Graph = &GraphClient{c: c}
	c.Admin = &AdminClient{c: c}
	return c
}

// do sends an HTTP request and decodes the response.
func (c *Client) do(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	c.checkVersionMismatch(resp)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		// Only attempt JSON error parsing when the server actually
		// advertised application/json. A non-JSON body (e.g. an HTML
		// 502 from a proxy, or a Go http.Error text/plain page) would
		// either silently "parse" through json.Unmarshal's lenient
		// mode producing a misleading zero-value errResp, or burn CPU
		// on a guaranteed failure. Skipping that round-trip leaves us
		// at the same fallback path (raw body in Message), only
		// without the wasted work.
		ct := resp.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "application/json") {
			var errResp ErrorResponse
			if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
				return &APIError{
					StatusCode: resp.StatusCode,
					Message:    errResp.Error,
					Code:       errResp.Code,
					Field:      errResp.Field,
				}
			}
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
		}
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// doNoContent sends a request that returns no body (e.g., 204).
func (c *Client) doNoContent(ctx context.Context, method, path string, body interface{}) error {
	return c.do(ctx, method, path, body, nil)
}

// doGet sends a GET request and decodes the response.
func (c *Client) doGet(ctx context.Context, path string, result interface{}) error {
	return c.do(ctx, http.MethodGet, path, nil, result)
}

// checkVersionMismatch reads the X-Hermem-API-Version header and calls
// OnVersionMismatch once if the server MAJOR differs from the SDK MAJOR.
func (c *Client) checkVersionMismatch(resp *http.Response) {
	if c.OnVersionMismatch == nil {
		return
	}
	c.versionCheckOnce.Do(func() {
		serverVersion := resp.Header.Get("X-Hermem-API-Version")
		if serverVersion == "" {
			return
		}
		serverMajor := parseMajor(serverVersion)
		sdkMajor := parseMajor(SDKVersion)
		if serverMajor != sdkMajor {
			c.OnVersionMismatch(serverVersion, SDKVersion)
		}
	})
}

// parseMajor extracts the MAJOR component from a semver string.
// Returns 0 for any unparseable input.
func parseMajor(v string) int {
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}
