package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// setupTestServer wires a Server backed by an in-memory SQLite DB,
// a stub Embedder, and a stub LLMExtractor (returning no entities so
// /ingest is a clean no-op without disk writes; /search and /query go
// through the deterministic stub embedder, which produces a vec
// derived from content length, so cosine is computed over a known
// input). The harness exposes the same routes as the production
// main.go mux, switched by r.URL.Path.
func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, vi := memDB(t)
	srv := NewServer(
		db, vi,
		&stubEmbedder{},
		&stubExtractor{resp: &ExtractionResult{Entities: nil}},
		0.99,
		RetrieveContextOptions{MaxDepth: 2, DepthCeiling: 5, MaxRetrievedNodes: 100},
		validRelationTypes,
	)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			srv.HandleHealth(w, r)
		case "/store":
			srv.HandleStore(w, r)
		case "/search":
			srv.HandleSearch(w, r)
		case "/retrieve":
			srv.HandleRetrieve(w, r)
		case "/ingest":
			srv.HandleIngest(w, r)
		case "/query":
			srv.HandleQuery(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		httpSrv.Close()
		db.Close()
	})
	return httpSrv
}

// doPost POSTs body (raw bytes) to baseURL+path on the test server
// with a JSON content type. Caller closes resp.Body. Empty body is
// preserved (an empty strings.NewReader is valid).
func doPost(t *testing.T, baseURL, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// decodeErr reads the structured ErrorResponse from a strict-decode
// rejection. Returns the (error, code, field) triple.
func decodeErr(t *testing.T, body io.Reader) ErrorResponse {
	t.Helper()
	var e ErrorResponse
	if err := json.NewDecoder(body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return e
}

// runCases drives a single handler through a list of named cases and
// asserts status + (code/field) for strict-decode rejections, or the
// message substring for non-strict (post-decode validation) rejections.
// The caller wires setupTestServer once per endpoint so the DB is
// reused across cases of that endpoint.
// wantStatus == http.StatusOK = "we expect 200 and don't read the body".
func runCases(t *testing.T, srv *httptest.Server, path string, cases []struct {
	name       string
	body       string
	wantStatus int
	wantCode   string
	wantField  string
	wantMsg    string // substring required on Error when wantCode == ""
}) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doPost(t, srv.URL, path, tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusOK {
				return
			}
			e := decodeErr(t, resp.Body)
			if tc.wantCode != "" {
				if e.Code != tc.wantCode {
					t.Errorf("code = %q, want %q", e.Code, tc.wantCode)
				}
				if e.Field != tc.wantField {
					t.Errorf("field = %q, want %q", e.Field, tc.wantField)
				}
			} else if tc.wantMsg != "" {
				if !strings.Contains(e.Error, tc.wantMsg) {
					t.Errorf("error msg = %q, want substring %q", e.Error, tc.wantMsg)
				}
			}
		})
	}
}

// ----- /store ---------------------------------------------------------

func TestServerStore(t *testing.T) {
	srv := setupTestServer(t)
	runCases(t, srv, "/store", []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
		wantMsg    string
	}{
		{"valid_minimal", `{"id":"f1","category":"world","content":"hello"}`, http.StatusOK, "", "", ""},
		{"valid_with_embedding", `{"id":"f2","category":"world","content":"hello","embedding":[0.1,0.2,0.3]}`, http.StatusOK, "", "", ""},
		{"unknown_field", `{"id":"f3","category":"world","content":"hello","foo":"bar"}`, http.StatusBadRequest, "unknown_field", "foo", ""},
		{"wrong_type_id", `{"id":123,"category":"world","content":"hello"}`, http.StatusBadRequest, "invalid_type", "id", ""},
		{"missing_content", `{"id":"f4","category":"world"}`, http.StatusBadRequest, "", "", "content"},
		{"empty_body", ``, http.StatusBadRequest, "empty_body", "", ""},
		{"malformed_json", `{"id":"x"`, http.StatusBadRequest, "bad_json", "", ""},
		{"trailing_data", `{"id":"f9","category":"world","content":"hello"}{"id":"f10","category":"world","content":"world"}`, http.StatusBadRequest, "trailing_data", "", ""},
		{"trailing_garbage", `{"id":"f11","category":"world","content":"hello"}garbage`, http.StatusBadRequest, "trailing_data", "", ""},
	})
}

// ----- /search --------------------------------------------------------

func TestServerSearch(t *testing.T) {
	srv := setupTestServer(t)
	runCases(t, srv, "/search", []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
		wantMsg    string
	}{
		{"valid_full", `{"query":"hello world","top_k":3}`, http.StatusOK, "", "", ""},
		{"valid_minimal", `{"query":"hello"}`, http.StatusOK, "", "", ""},
		{"unknown_field", `{"query":"hello","surprise":true}`, http.StatusBadRequest, "unknown_field", "surprise", ""},
		{"wrong_type_query", `{"query":42}`, http.StatusBadRequest, "invalid_type", "query", ""},
		{"wrong_type_top_k", `{"query":"hi","top_k":"three"}`, http.StatusBadRequest, "invalid_type", "top_k", ""},
		{"missing_query", `{}`, http.StatusBadRequest, "", "", "query required"},
		{"empty_body", ``, http.StatusBadRequest, "empty_body", "", ""},
	})
}

// ----- /retrieve ------------------------------------------------------

func TestServerRetrieve(t *testing.T) {
	srv := setupTestServer(t)
	runCases(t, srv, "/retrieve", []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
		wantMsg    string
	}{
		{"valid_minimal", `{"seed_ids":["f1"]}`, http.StatusOK, "", "", ""},
		{"valid_with_depth", `{"seed_ids":["f1"],"max_depth":3}`, http.StatusOK, "", "", ""},
		{"unknown_field", `{"seed_ids":["f1"],"surprise_field":true}`, http.StatusBadRequest, "unknown_field", "surprise_field", ""},
		{"wrong_type_seed_ids", `{"seed_ids":"not-an-array","max_depth":2}`, http.StatusBadRequest, "invalid_type", "seed_ids", ""},
		{"missing_seed_ids", `{"max_depth":3}`, http.StatusBadRequest, "", "", "seed_ids required"},
		{"empty_body", ``, http.StatusBadRequest, "empty_body", "", ""},
	})
}

// ----- /ingest --------------------------------------------------------

func TestServerIngest(t *testing.T) {
	srv := setupTestServer(t)
	runCases(t, srv, "/ingest", []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
		wantMsg    string
	}{
		{"valid_minimal", `{"dialog":"User: hello\nAssistant: hi"}`, http.StatusOK, "", "", ""},
		{"unknown_field", `{"dialog":"x","unexpected_field":"y"}`, http.StatusBadRequest, "unknown_field", "unexpected_field", ""},
		{"wrong_type_dialog", `{"dialog":42}`, http.StatusBadRequest, "invalid_type", "dialog", ""},
		{"missing_dialog", `{}`, http.StatusBadRequest, "", "", "dialog required"},
		{"empty_body", ``, http.StatusBadRequest, "empty_body", "", ""},
	})
}

// ----- /query ---------------------------------------------------------

func TestServerQuery(t *testing.T) {
	srv := setupTestServer(t)
	runCases(t, srv, "/query", []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
		wantMsg    string
	}{
		{"valid_minimal", `{"query":"hello world"}`, http.StatusOK, "", "", ""},
		{"unknown_field", `{"query":"hi","surprise":true}`, http.StatusBadRequest, "unknown_field", "surprise", ""},
		{"wrong_type_query", `{"query":[]}`, http.StatusBadRequest, "invalid_type", "query", ""},
		{"missing_query", `{}`, http.StatusBadRequest, "", "", "query required"},
		{"empty_body", ``, http.StatusBadRequest, "empty_body", "", ""},
	})
}

// ----- /health --------------------------------------------------------

// /health stays non-strict (it consumes no body), but its status code
// and JSON shape remain stable.
func TestServerHealthAlwaysOK(t *testing.T) {
	srv := setupTestServer(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, err := http.NewRequest(method, srv.URL+"/health", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do %s /health: %v", method, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s /health: status = %d, want 200", method, resp.StatusCode)
		}
		resp.Body.Close()
	}
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("Get /health: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want %q", body["status"], "ok")
	}
}

// ----- /store method gate --------------------------------------------

func TestServerStoreMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/store", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
	e := decodeErr(t, resp.Body)
	if e.Error != "method not allowed" {
		t.Errorf("error msg = %q, want %q", e.Error, "method not allowed")
	}
	if e.Code != "" {
		t.Errorf("expected unset Code for non-strict error, got %q", e.Code)
	}
}
