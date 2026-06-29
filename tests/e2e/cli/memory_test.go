package cli

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestMemoryStore(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.RunWithStdin(t, `{"id":"e1","category":"invalid","content":"test"}`, "memory", "store")
	result.MustFail(t)
}

func TestMemoryStoreMalformedJSON(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.RunWithStdin(t, `{not json}`, "memory", "store")
	result.MustFail(t)
}

func TestMemoryStoreMissingID(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.RunWithStdin(t, `{"category":"world","content":"test"}`, "memory", "store")
	result.MustFail(t)
}

func TestMemorySearch(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Store first
	cli.RunWithStdin(t, `{"id":"e1","category":"world","content":"Paris is the capital of France"}`, "memory", "store").MustSucceed(t)

	// Search
	result := cli.RunWithStdin(t, `{"query":"capital of France","top_k":5}`, "memory", "search")
	result.MustSucceed(t)
	if result.Stdout == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestMemoryQuery(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Store first
	cli.RunWithStdin(t, `{"id":"e1","category":"world","content":"Paris is the capital of France"}`, "memory", "store").MustSucceed(t)

	// Query
	result := cli.RunWithStdin(t, `{"query":"What is the capital of France?"}`, "memory", "query")
	result.MustSucceed(t)
}

func TestMemoryEdge(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Store entities
	cli.RunWithStdin(t, `{"id":"e1","category":"world","content":"Paris"}`, "memory", "store").MustSucceed(t)
	cli.RunWithStdin(t, `{"id":"e2","category":"world","content":"France"}`, "memory", "store").MustSucceed(t)

	// Create edge
	result := cli.RunWithStdin(t, `{"source_id":"e1","target_id":"e2","relation_type":"part_of"}`, "memory", "edge")
	result.MustSucceed(t)
}

func TestMemoryEdgeMissingEntity(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.RunWithStdin(t, `{"source_id":"missing","target_id":"also-missing","relation_type":"related_to"}`, "memory", "edge")
	result.MustFail(t)
}

func TestMemoryIngest(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.RunWithStdin(t, `{"dialog":"User: What is Go?\nAssistant: Go is a statically typed language."}`, "memory", "ingest")
	// Ingest may fail without an LLM provider, but should not panic
	_ = result
}

func TestMemoryRetrieve(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Store first
	cli.RunWithStdin(t, `{"id":"e1","category":"world","content":"Paris is the capital of France"}`, "memory", "store").MustSucceed(t)

	// Retrieve (may need search first for seeds)
	result := cli.RunWithStdin(t, `{"seed_ids":["e1"]}`, "memory", "retrieve")
	result.MustSucceed(t)
}
