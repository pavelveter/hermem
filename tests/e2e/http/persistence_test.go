package http

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestPersistenceAcrossRestarts(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))

	// Start server, store data, stop server
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	client.Post(t, "/store", map[string]interface{}{
		"id":       "e1",
		"category": "world",
		"content":  "Paris is the capital of France",
	})

	// Server stops via t.Cleanup

	// Start new server with same database
	srv2 := helpers.StartServer(t, dir)
	client2 := helpers.NewHTTPClient(srv2.URL)

	// Search should find the stored entity
	resp := client2.Post(t, "/search", map[string]interface{}{
		"query": "capital of France",
		"top_k": 5,
	})
	helpers.MustStatus(t, resp, 200)
}

func TestCLIHTTPInteroperability(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))

	// Store via CLI (JSON goes via stdin, not as command-line arg)
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)
	cli.RunWithStdin(t, `{"id":"e1","category":"world","content":"Paris is the capital of France"}`, "memory", "store").MustSucceed(t)

	// Query via HTTP
	srv := helpers.StartServer(t, dir)
	client := helpers.NewHTTPClient(srv.URL)

	resp := client.Post(t, "/search", map[string]interface{}{
		"query": "capital of France",
		"top_k": 5,
	})
	helpers.MustStatus(t, resp, 200)
}
