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
		taskSchema(),
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
		case "/edge":
			srv.HandleEdge(w, r)
		case "/task/status":
			srv.HandleTaskStatus(w, r)
		case "/task/executable":
			srv.HandleTaskExecutable(w, r)
		case "/task/next":
			srv.HandleTaskExecutable(w, r)
		case "/task/list":
			srv.HandleTaskList(w, r)
		case "/task/show":
			srv.HandleTaskShow(w, r)
		case "/task/dep":
			srv.HandleTaskDep(w, r)
		case "/task/tree":
			srv.HandleTaskTree(w, r)
		case "/task/create":
			srv.HandleTaskCreate(w, r)
		case "/task/rollback":
			srv.HandleTaskRollback(w, r)
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

// ----- /task/status ---------------------------------------------------

func TestServerTaskStatus(t *testing.T) {
	srv := setupTestServer(t)

	// Store a task entity first.
	resp := doPost(t, srv.URL, "/store", `{"id":"ts1","category":"task","content":"test task"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("store task: status = %d, want 200", resp.StatusCode)
	}

	// Valid status update.
	resp = doPost(t, srv.URL, "/task/status", `{"id":"ts1","status":"running"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("update running: status = %d, want 204", resp.StatusCode)
	}

	// Invalid status.
	resp = doPost(t, srv.URL, "/task/status", `{"id":"ts1","status":"bogus"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("invalid status: status = %d, want 422", resp.StatusCode)
	}

	// Non-existent task.
	resp = doPost(t, srv.URL, "/task/status", `{"id":"nope","status":"pending"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing task: status = %d, want 400", resp.StatusCode)
	}

	runCases(t, srv, "/task/status", []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
		wantMsg    string
	}{
		{"empty_body", ``, http.StatusBadRequest, "empty_body", "", ""},
		{"unknown_field", `{"id":"x","status":"pending","surprise":true}`, http.StatusBadRequest, "unknown_field", "surprise", ""},
		{"missing_id", `{"status":"running"}`, http.StatusBadRequest, "", "", "id, status required"},
		{"missing_status", `{"id":"ts1"}`, http.StatusBadRequest, "", "", "id, status required"},
	})
}

// ----- /task/executable -----------------------------------------------

func TestServerTaskExecutable(t *testing.T) {
	srv := setupTestServer(t)

	// Store three tasks in a chain: A blocks B, B blocks C.
	for _, body := range []string{
		`{"id":"ea","category":"task","content":"step A"}`,
		`{"id":"eb","category":"task","content":"step B"}`,
		`{"id":"ec","category":"task","content":"step C"}`,
	} {
		resp := doPost(t, srv.URL, "/store", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("store: status = %d", resp.StatusCode)
		}
	}
	// B blocked_by A, C blocked_by B.
	doPost(t, srv.URL, "/edge", `{"source_id":"eb","target_id":"ea","relation_type":"blocked_by"}`).Body.Close()
	doPost(t, srv.URL, "/edge", `{"source_id":"ec","target_id":"eb","relation_type":"blocked_by"}`).Body.Close()

	// Initially A has no blockers → executable.
	resp := doPost(t, srv.URL, "/task/executable", ``)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial: status = %d", resp.StatusCode)
	}
	var result TaskExecutableResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "ea" {
		ids := make([]string, len(result.Tasks))
		for i, e := range result.Tasks {
			ids[i] = e.ID
		}
		t.Errorf("initial executable = %v, want [ea]", ids)
	}

	// Complete A → B should be executable.
	doPost(t, srv.URL, "/task/status", `{"id":"ea","status":"completed"}`).Body.Close()

	resp = doPost(t, srv.URL, "/task/executable", ``)
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "eb" {
		ids := make([]string, len(result.Tasks))
		for i, e := range result.Tasks {
			ids[i] = e.ID
		}
		t.Errorf("after A completed: executable = %v, want [eb]", ids)
	}

	// Complete B → C should be executable.
	doPost(t, srv.URL, "/task/status", `{"id":"eb","status":"completed"}`).Body.Close()

	resp = doPost(t, srv.URL, "/task/executable", ``)
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "ec" {
		ids := make([]string, len(result.Tasks))
		for i, e := range result.Tasks {
			ids[i] = e.ID
		}
		t.Errorf("after B completed: executable = %v, want [ec]", ids)
	}
}

func TestServerTaskExecutableGoalScoped(t *testing.T) {
	srv := setupTestServer(t)

	// Two independent tasks: g1 blocked by gx, g2 blocked by gy.
	doPost(t, srv.URL, "/store", `{"id":"g1","category":"task","content":"goal 1"}`).Body.Close()
	doPost(t, srv.URL, "/store", `{"id":"gx","category":"task","content":"step x"}`).Body.Close()
	doPost(t, srv.URL, "/store", `{"id":"g2","category":"task","content":"goal 2"}`).Body.Close()
	doPost(t, srv.URL, "/store", `{"id":"gy","category":"task","content":"step y"}`).Body.Close()
	doPost(t, srv.URL, "/edge", `{"source_id":"g1","target_id":"gx","relation_type":"blocked_by"}`).Body.Close()
	doPost(t, srv.URL, "/edge", `{"source_id":"g2","target_id":"gy","relation_type":"blocked_by"}`).Body.Close()

	// With goal_id=g1: dep_tree = {g1, gx}. gx has no blockers → executable.
	resp := doPost(t, srv.URL, "/task/executable?goal_id=g1", ``)
	defer resp.Body.Close()
	var result TaskExecutableResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "gx" {
		ids := make([]string, len(result.Tasks))
		for i, e := range result.Tasks {
			ids[i] = e.ID
		}
		t.Errorf("goal1 scoped: executable = %v, want [gx]", ids)
	}

	// With goal_id=g2: dep_tree = {g2, gy}. gy has no blockers → executable.
	resp = doPost(t, srv.URL, "/task/executable?goal_id=g2", ``)
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "gy" {
		ids := make([]string, len(result.Tasks))
		for i, e := range result.Tasks {
			ids[i] = e.ID
		}
		t.Errorf("goal2 scoped: executable = %v, want [gy]", ids)
	}
}

func TestServerTaskStatusMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/status", nil)
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
}

func TestServerTaskExecutableMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/executable", nil)
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
}

// ----- /task/next ------------------------------------------------------

func TestServerTaskNext(t *testing.T) {
	srv := setupTestServer(t)
	for _, body := range []string{
		`{"id":"na","category":"task","content":"A"}`,
		`{"id":"nb","category":"task","content":"B"}`,
	} {
		doPost(t, srv.URL, "/store", body).Body.Close()
	}
	doPost(t, srv.URL, "/edge", `{"source_id":"nb","target_id":"na","relation_type":"blocked_by"}`).Body.Close()
	resp := doPost(t, srv.URL, "/task/next", `{}`)
	defer resp.Body.Close()
	var result TaskExecutableResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "na" {
		ids := make([]string, len(result.Tasks))
		for i, e := range result.Tasks {
			ids[i] = e.ID
		}
		t.Errorf("task/next = %v, want [na]", ids)
	}
}
func TestServerTaskNextMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/next", nil)
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
}

// ----- /task/list ------------------------------------------------------

func TestServerTaskList(t *testing.T) {
	srv := setupTestServer(t)
	for _, body := range []string{
		`{"id":"t1","category":"task","content":"A"}`,
		`{"id":"t2","category":"task","content":"B"}`,
	} {
		doPost(t, srv.URL, "/store", body).Body.Close()
	}
	doPost(t, srv.URL, "/task/status", `{"id":"t1","status":"completed"}`).Body.Close()

	resp := doPost(t, srv.URL, "/task/list", `{}`)
	defer resp.Body.Close()
	var result TaskExecutableResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 2 {
		t.Errorf("list all = %d, want 2", len(result.Tasks))
	}

	resp = doPost(t, srv.URL, "/task/list", `{"status":"pending"}`)
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "t2" {
		ids := make([]string, len(result.Tasks))
		for i, e := range result.Tasks {
			ids[i] = e.ID
		}
		t.Errorf("list pending = %v, want [t2]", ids)
	}

	resp = doPost(t, srv.URL, "/task/list", `{"status":"completed"}`)
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "t1" {
		ids := make([]string, len(result.Tasks))
		for i, e := range result.Tasks {
			ids[i] = e.ID
		}
		t.Errorf("list completed = %v, want [t1]", ids)
	}
}
func TestServerTaskListMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/list", nil)
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
}

// ----- /task/show ------------------------------------------------------

func TestServerTaskShow(t *testing.T) {
	srv := setupTestServer(t)
	doPost(t, srv.URL, "/store", `{"id":"t1","category":"task","content":"A"}`).Body.Close()
	doPost(t, srv.URL, "/store", `{"id":"t2","category":"task","content":"B"}`).Body.Close()
	doPost(t, srv.URL, "/edge", `{"source_id":"t1","target_id":"t2","relation_type":"blocked_by"}`).Body.Close()
	doPost(t, srv.URL, "/edge", `{"source_id":"t1","target_id":"t2","relation_type":"recovers_via"}`).Body.Close()

	resp := doPost(t, srv.URL, "/task/show", `{"id":"t1"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("show: status = %d", resp.StatusCode)
	}
	var result TaskShowResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Entity.ID != "t1" {
		t.Errorf("entity id = %q, want t1", result.Entity.ID)
	}
	if len(result.BlockedBy) != 1 || result.BlockedBy[0].TargetID != "t2" {
		t.Errorf("blocked_by = %+v, want [t2]", result.BlockedBy)
	}
	if len(result.RecoversVia) != 1 || result.RecoversVia[0].TargetID != "t2" {
		t.Errorf("recovers_via = %+v, want [t2]", result.RecoversVia)
	}

	resp = doPost(t, srv.URL, "/task/show", `{"id":"nope"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing: status = %d, want 400", resp.StatusCode)
	}
}
func TestServerTaskShowMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/show", nil)
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
}

// ----- /task/dep -------------------------------------------------------

func TestServerTaskDep(t *testing.T) {
	srv := setupTestServer(t)
	doPost(t, srv.URL, "/store", `{"id":"d1","category":"task","content":"X"}`).Body.Close()
	doPost(t, srv.URL, "/store", `{"id":"d2","category":"task","content":"Y"}`).Body.Close()

	resp := doPost(t, srv.URL, "/task/dep", `{"source_id":"d1","target_id":"d2","add":true}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add dep: status = %d", resp.StatusCode)
	}

	resp = doPost(t, srv.URL, "/task/dep", `{"source_id":"d1","target_id":"d2","add":false}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove dep: status = %d", resp.StatusCode)
	}
}
func TestServerTaskDepMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/dep", nil)
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
}

// ----- /task/rollback --------------------------------------------------

func TestServerTaskRollback(t *testing.T) {
	srv := setupTestServer(t)
	doPost(t, srv.URL, "/store", `{"id":"fail","category":"task","content":"F"}`).Body.Close()
	doPost(t, srv.URL, "/store", `{"id":"fix","category":"task","content":"R"}`).Body.Close()
	doPost(t, srv.URL, "/edge", `{"source_id":"fail","target_id":"fix","relation_type":"recovers_via"}`).Body.Close()

	resp := doPost(t, srv.URL, "/task/rollback", `{"id":"fail"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rollback: status = %d", resp.StatusCode)
	}
	var result TaskRollbackResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.RollbackTaskID != "fix" {
		t.Errorf("rollback_id = %q, want fix", result.RollbackTaskID)
	}

	resp = doPost(t, srv.URL, "/task/rollback", `{"id":"none"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("missing rollback: status = %d, want 200", resp.StatusCode)
	}
}
func TestServerTaskRollbackMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/rollback", nil)
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
}

// ----- /task/create ------------------------------------------------------

func TestServerTaskCreate(t *testing.T) {
	srv := setupTestServer(t)
	doPost(t, srv.URL, "/store", `{"id":"ctx1","category":"task","content":"ctx"}`).Body.Close()

	resp := doPost(t, srv.URL, "/task/create", `{"content":"new task","context_ids":["ctx1"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: status = %d", resp.StatusCode)
	}
	var result TaskCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("status = %q, want ok", result.Status)
	}
	if result.ID == "" {
		t.Error("expected non-empty id")
	}

	resp = doPost(t, srv.URL, "/task/create", `{"content":""}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty content: status = %d, want 400", resp.StatusCode)
	}
}

func TestServerTaskCreateMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/create", nil)
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
}

func TestServerTaskTree(t *testing.T) {
	srv := setupTestServer(t)
	doPost(t, srv.URL, "/store", `{"id":"g","category":"task","content":"goal"}`).Body.Close()
	doPost(t, srv.URL, "/store", `{"id":"a","category":"task","content":"blocker"}`).Body.Close()
	doPost(t, srv.URL, "/edge", `{"source_id":"a","target_id":"g","relation_type":"blocked_by"}`).Body.Close()

	resp := doPost(t, srv.URL, "/task/tree", `{"goal_id":"g"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tree: status = %d", resp.StatusCode)
	}
	var result TaskTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Tree == "" {
		t.Error("expected non-empty tree")
	}
	if !strings.Contains(result.Tree, "[g]") || !strings.Contains(result.Tree, "[a]") {
		t.Errorf("unexpected tree: %q", result.Tree)
	}
}

func TestServerTaskTreeMethodNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/task/tree", nil)
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
}

// ----- /store method gate ----------------------------------------------

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
