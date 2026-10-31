package cli

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestHybridSearchKeywordE2E(t *testing.T) {
	helpers.SkipIfNoEmbedder(t)
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	cli.RunWithStdin(t, `{"id":"kw-1","category":"world","content":"unique-keyword-xyz"}`, "memory", "store").MustSucceed(t)

	result := cli.RunWithStdin(t, `{"query":"unique-keyword-xyz","top_k":5}`, "memory", "search")
	result.MustSucceed(t)
	if result.Stdout == "" {
		t.Fatal("expected non-empty search result")
	}
}
