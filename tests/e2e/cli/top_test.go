package cli

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestVersion(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.Run(t, "version")
	result.MustSucceed(t)
	if result.Stdout == "" {
		t.Fatal("expected non-empty version output")
	}
}

func TestHealth(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.Run(t, "health")
	result.MustSucceed(t)
}

func TestMetrics(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.Run(t, "metrics")
	result.MustSucceed(t)
}

func TestDiagnose(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.Run(t, "diagnose")
	result.MustSucceed(t)
}
