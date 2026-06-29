package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/pavelveter/hermem/src/internal/httputil"
)

// httpClient is the internal unified HTTP client for every AI provider
// (OllamaEmbedder, OpenAIEmbedder, OllamaLLMExtractor, OpenAILLMExtractor,
// OllamaReranker, OpenAIReranker). It owns the three concerns that were
// duplicated in every client before §9:
//
//   - baseURL  — already TrimRight'd of any trailing / so path concatenation
//     never produces `//`.
//   - apiKey   — empty for Ollama (no Authorization header), populated for
//     OpenAI (Bearer header attached below).
//   - resilient — the shared *ResilientClient that retries on 5xx/429/network
//     failures and re-attaches the request body via GetBody.
//
// Reusing ResilientClient unchanged keeps the retry semantics verified by the
// existing tests in client_test.go. Adding a NEW retry strategy here would
// duplicate that test surface for no benefit.
//
// httpClient is package-private on purpose. The 6 public constructors keep
// their stable New* signatures so callers in src/internal/config.go (and any
// tests) remain unaffected. Only the unused fields on the per-client structs
// (their own `client *http.Client` + `resilient *ResilientClient`) collapse
// into one `http *httpClient` field; the public `BaseURL`/`APIKey`/`Model`
// fields stay exported for diagnostic / display purposes.
type httpClient struct {
	baseURL   string
	apiKey    string
	resilient *ResilientClient
}

// newHTTPClient builds the helper. Defaulting of baseURL/apiKey is the
// caller's responsibility (the OpenAI vs Ollama defaults differ and live in
// each provider's constructor). What newHTTPClient owns is:
//
//   - Trimming any trailing `/` from baseURL so path joins are deterministic.
//   - Building a fresh *http.Client with the supplied timeout so a fixed
//     timeout can be re-applied per request without sharing state.
//   - Wiring that client into a ResilientClient with the supplied attempt
//     count (1 initial + N retries, where N == attempts-1).
func newHTTPClient(baseURL, apiKey string, timeout time.Duration, attempts int) *httpClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	// Custom transport with socket-level deadlines prevents goroutines
	// from hanging indefinitely in syscalls when a remote peer stalls
	// after sending headers. Without this, a hung Ollama instance can
	// block a goroutine for up to 2 hours (OS TCP keepalive timeout).
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: timeout,
		}).DialContext,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
	}
	c := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	return &httpClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		resilient: NewResilientClient(c, attempts, DefaultBackoffs()),
	}
}

// doPOST marshals reqBody to JSON, POSTs to baseURL+path, and on a 2xx
// response streams the wire into dst via json.NewDecoder. On a non-2xx
// response the body is fully read (small enough — usually an error message)
// and returned as `fmt.Errorf("%d: %s", status, body)` so the caller can
// prefix it with their domain tag (e.g. `fmt.Errorf("ollama embed: %w", err)`).
//
// Body replay across retries is the caller's responsibility in the abstract
// sense, but here the helper itself sets `req.GetBody` to a closure that
// re-emits the marshalled bytes from `captured`, so the second-and-later
// attempts get a fresh *bytes.Reader rather than a consumed io.ReadCloser.
// Without this, ResilientClient.Do's retry path would fail at `req.GetBody()`
// on attempt #2.
//
// The returned error captures three failure modes uniformly — bad-context,
// transport, non-2xx, body decode — under a single error chain, so the caller
// can wrap with one `fmt.Errorf("domain: %w", err)` and the wrapping caller
// sees the underlying text via err.Error(). Lost are the pre-§9 per-mode
// prefixes ("ollama embed: build request: …" vs "ollama embed decode: …");
// the wire text differs only in the tail word ("build request" / "decode"),
// which was never load-bearing for any caller.
//
// §2 AUDIT CLOSURE: the response-body read paths are io.LimitReader-capped
// at httputil.MaxResponseBodyBytes (16 MiB) for the 2xx decode path and at
// httputil's internal 4xx-snippet cap for the non-2xx path. A hostile or
// buggy downstream provider shipping a 4 GB body would otherwise allocate
// the entire stream into RAM before the helper could react. The 16 MiB
// cap is generous for legitimate AI provider responses (single embedding
// vector + LLM JSON envelope stay well below) while bounding the worst
// case to a manageable allocation. See httputil.SafeStreamFetch
// documentation for the parallel helper that owns the URL+ctx signature.
// maxErrorSnippetBytes caps the body fragment included in the non-2xx
// error message returned from doPOST. Mirrors httputil's internal
// safeStreamFetchSnippetBytes but is duplicated here so ai/http.go has
// no behavioral dependency on a private symbol of httputil. A hostile
// downstream provider shipping a 100 MB 4xx body would otherwise be
// dutifully read-and-included into the error message, amplifying RAM.
// 16 KiB comfortably holds the typical error JSON envelope
// ({"error": "..."} on Ollama/OpenAI).
const maxErrorSnippetBytes int64 = 16 * 1024

func (c *httpClient) doPOST(ctx context.Context, path string, reqBody, dst any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	captured := body
	req.Body = io.NopCloser(bytes.NewReader(captured))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(captured)), nil
	}
	resp, err := c.resilient.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 4xx / 5xx snippet: cap at a constant so a hostile provider
		// cannot amplify RAM through the error-message read.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippetBytes))
		return fmt.Errorf("%d: %s", resp.StatusCode, string(b))
	}
	// 2xx path: read with body-cap via LimitReader, then check
	// overflow against httputil.MaxResponseBodyBytes before
	// json.Unmarshal. If cap exceeded, return httputil.ErrResponseTooLarge
	// wrapped so callers can errors.Is against the sentinel.
	maxBytes := httputil.MaxResponseBodyBytes
	bodyBuf, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if int64(len(bodyBuf)) > maxBytes {
		return fmt.Errorf("%w: body=%d bytes (cap=%d)", httputil.ErrResponseTooLarge, len(bodyBuf), maxBytes)
	}
	return json.Unmarshal(bodyBuf, dst)
}

// doGET issues a GET to baseURL+path and returns the response. On a non-2xx
// response the body is fully read and returned as an error. Used by Ping
// health checks.
func (c *httpClient) doGET(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return c.resilient.Do(ctx, req)
}
