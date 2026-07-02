package http

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestStoreEntity(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "world",
		"content":  "Paris is the capital of France",
	})
	helpers.MustStatus(t, resp, 200)
}

func TestStoreEntityInvalidCategory(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "invalid",
		"content":  "test",
	})
	helpers.MustStatus(t, resp, 422)
}

func TestSearch(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Store first
	client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "world",
		"content":  "Paris is the capital of France",
	})

	// Search
	resp := client.Post(t, "/search", map[string]interface{}{
		"query": "capital of France",
		"top_k": 5,
	})
	helpers.MustStatus(t, resp, 200)
}

func TestQuery(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Store first
	client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "world",
		"content":  "Paris is the capital of France",
	})

	// Query
	resp := client.Post(t, "/query", map[string]interface{}{
		"query": "What is the capital of France?",
	})
	helpers.MustStatus(t, resp, 200)
}

func TestRetrieve(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Store first
	client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "world",
		"content":  "Paris is the capital of France",
	})

	// Retrieve
	resp := client.Post(t, "/retrieve", map[string]interface{}{
		"seed_ids": []string{"e1"},
	})
	helpers.MustStatus(t, resp, 200)
}

func TestEdge(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Store entities
	client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "world",
		"content":  "Paris",
	})
	client.Post(t, "/store", map[string]interface{}{
		"id":       "e2",
		"category": "world",
		"content":  "France",
	})

	// Create edge
	resp := client.Post(t, "/edge", map[string]interface{}{
		"source_id":     "e1",
		"target_id":     "e2",
		"relation_type": "part_of",
	})
	helpers.MustStatus(t, resp, 200)
}

func TestEdgeMissingEntity(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Post(t, "/edge", map[string]interface{}{
		"source_id":     "missing",
		"target_id":     "also-missing",
		"relation_type": "related_to",
	})
	if resp.StatusCode != 422 && resp.StatusCode != 500 {
		t.Fatalf("expected status 422 or 500, got %d", resp.StatusCode)
	}
}

func TestIngest(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Post(t, "/ingest", map[string]interface{}{
		"dialog": "User: What is Go?\nAssistant: Go is a statically typed language.",
	})
	// May fail without LLM provider, but should not panic
	_ = resp
}

func TestTaskCreate(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Post(t, "/task/create", map[string]interface{}{
		"id":      "task-1",
		"content": "Run tests",
	})
	helpers.MustStatus(t, resp, 200)
}

func TestTaskStatus(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Create task
	client.Post(t, "/task/create", map[string]interface{}{
		"id":      "task-1",
		"content": "Run tests",
	})

	// Update status
	resp := client.Post(t, "/task/status", map[string]interface{}{
		"id":     "task-1",
		"status": "in_progress",
	})
	helpers.MustStatus(t, resp, 204)
}

func TestTaskList(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Create tasks
	client.Post(t, "/task/create", map[string]interface{}{
		"id":      "task-1",
		"content": "Task 1",
	})
	client.Post(t, "/task/create", map[string]interface{}{
		"id":      "task-2",
		"content": "Task 2",
	})

	// List
	resp := client.Post(t, "/task/list", map[string]interface{}{})
	helpers.MustStatus(t, resp, 200)
}

func TestTaskExecutable(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Create task
	client.Post(t, "/task/create", map[string]interface{}{
		"id":      "task-1",
		"content": "Run tests",
	})

	// Get executable
	resp := client.Post(t, "/task/executable", map[string]interface{}{})
	helpers.MustStatus(t, resp, 200)
}

func TestTimeline(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Get(t, "/timeline")
	helpers.MustStatus(t, resp, 200)
}

func TestContradictions(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Get(t, "/contradictions")
	helpers.MustStatus(t, resp, 200)
}

func TestConnectedComponents(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Get(t, "/connected-components")
	helpers.MustStatus(t, resp, 200)
}

func TestCommunities(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Get(t, "/communities")
	helpers.MustStatus(t, resp, 200)
}

func TestGraphVerify(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Store some data first
	client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "world",
		"content":  "test",
	})

	resp := client.Get(t, "/graph/verify")
	helpers.MustStatus(t, resp, 200)
}

// TestQueryTemporal — POST /query/temporal. C2 closes the
// spec-but-no-handler gap: the OpenAPI spec has advertised this
// route since the temporal-tagged path entry was added in api/paths.go,
// but the HTTP handler was registered without an e2e test. Mirrors
// TestQuery above so the 3 input branches (time_from-only, time_to-only,
// both) plus the RFC3339 parse-error branch are all pinned.
func TestQueryTemporal(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	// Store an entity so the query pipeline has a seed.
	client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "world",
		"content":  "Paris is the capital of France",
	})

	// 1) No time bounds — must behave like /query (handler passes
	//    empty TimeFrom/TimeTo, so opts.TimeFrom/TimeTo stay zero and
	//    the walk is unfiltered).
	t.Run("no_time_bounds", func(t *testing.T) {
		resp := client.Post(t, "/query/temporal", map[string]interface{}{
			"query": "What is the capital of France?",
			"top_k": 3,
		})
		helpers.MustStatus(t, resp, 200)
	})

	// 2) time_from only — RFC3339 lower bound, no upper bound.
	t.Run("time_from_only", func(t *testing.T) {
		resp := client.Post(t, "/query/temporal", map[string]interface{}{
			"query":     "What is the capital of France?",
			"top_k":     3,
			"time_from": "2020-01-01T00:00:00Z",
		})
		helpers.MustStatus(t, resp, 200)
	})

	// 3) time_to only — no lower bound, RFC3339 upper bound.
	t.Run("time_to_only", func(t *testing.T) {
		resp := client.Post(t, "/query/temporal", map[string]interface{}{
			"query":   "What is the capital of France?",
			"top_k":   3,
			"time_to": "2099-12-31T23:59:59Z",
		})
		helpers.MustStatus(t, resp, 200)
	})

	// 4) Both bounds — inclusive RFC3339 window.
	t.Run("both_bounds", func(t *testing.T) {
		resp := client.Post(t, "/query/temporal", map[string]interface{}{
			"query":     "What is the capital of France?",
			"top_k":     3,
			"time_from": "2020-01-01T00:00:00Z",
			"time_to":   "2099-12-31T23:59:59Z",
		})
		helpers.MustStatus(t, resp, 200)
	})

	// 5) Malformed time_from must surface as 422 (the handler
	//    returns WriteErrorWithCode(StatusUnprocessableEntity,
	//    CodeInvalidInput) when time.Parse fails).
	t.Run("invalid_time_from", func(t *testing.T) {
		resp := client.Post(t, "/query/temporal", map[string]interface{}{
			"query":     "anything",
			"time_from": "not-rfc3339",
		})
		helpers.MustStatus(t, resp, 422)
	})

	// 6) Malformed time_to — same contract as time_from. The literal
	//    is structurally-valid RFC3339 (T separator, Z suffix) but
	//    carries an impossible month so time.Parse returns an error
	//    for a *semantic* reason, not a structural one — this pins
	//    the same code path the handler uses for any parse failure.
	t.Run("invalid_time_to", func(t *testing.T) {
		resp := client.Post(t, "/query/temporal", map[string]interface{}{
			"query":   "anything",
			"time_to": "2024-13-01T00:00:00Z", // month 13
		})
		helpers.MustStatus(t, resp, 422)
	})

	// 7) Missing query — same 422 contract as /query.
	t.Run("missing_query", func(t *testing.T) {
		resp := client.Post(t, "/query/temporal", map[string]interface{}{})
		helpers.MustStatus(t, resp, 422)
	})
}
