package scenarios

import (
	"testing"

	helpers "github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestBasicMemoryScenario(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
	helpers.RunScenario(t, dir, "../../../testdata/scenarios/basic_memory.yaml")
}

func TestTaskPlannerScenario(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	helpers.RunScenario(t, dir, "../../../testdata/scenarios/task_planner.yaml")
}

func TestAllScenarios(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	helpers.RunAllScenarios(t, dir, "../../../testdata/scenarios")
}
