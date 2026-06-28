package cli

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestGraphVerify(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Store some data first
	cli.RunWithStdin(t, `{"id":"e1","category":"world","content":"test"}`, "memory", "store").MustSucceed(t)

	result := cli.Run(t, "graph", "verify")
	result.MustSucceed(t)
}

func TestGraphComponents(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.Run(t, "graph", "components")
	result.MustSucceed(t)
}

func TestGraphCommunities(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.Run(t, "graph", "communities")
	result.MustSucceed(t)
}

func TestGraphContradictions(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.Run(t, "graph", "contradictions")
	result.MustSucceed(t)
}

func TestGraphProvenance(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Provenance requires at least one filter - test that it returns proper error
	result := cli.Run(t, "graph", "provenance")
	result.MustFail(t) // Should fail without any filter
}
