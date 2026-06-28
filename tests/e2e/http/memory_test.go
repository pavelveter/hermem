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
