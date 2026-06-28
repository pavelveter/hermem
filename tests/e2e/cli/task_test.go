package cli

import (
	"testing"

	"github.com/pavelveter/hermem/tests/e2e/helpers"
)

func TestTaskCreate(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	result := cli.RunWithStdin(t, `{"id":"task-1","content":"Run tests"}`, "task", "create")
	result.MustSucceed(t)
}

func TestTaskStatus(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Create task
	cli.RunWithStdin(t, `{"id":"task-1","content":"Run tests"}`, "task", "create").MustSucceed(t)

	// Update status
	result := cli.RunWithStdin(t, `{"id":"task-1","status":"in_progress"}`, "task", "status")
	result.MustSucceed(t)
}

func TestTaskList(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Create tasks
	cli.RunWithStdin(t, `{"id":"task-1","content":"Task 1"}`, "task", "create").MustSucceed(t)
	cli.RunWithStdin(t, `{"id":"task-2","content":"Task 2"}`, "task", "create").MustSucceed(t)

	// List all
	result := cli.RunWithStdin(t, `{}`, "task", "list")
	result.MustSucceed(t)
}

func TestTaskShow(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Create task
	cli.RunWithStdin(t, `{"id":"task-1","content":"Run tests"}`, "task", "create").MustSucceed(t)

	// Show task
	result := cli.RunWithStdin(t, `{"id":"task-1"}`, "task", "show")
	result.MustSucceed(t)
}

func TestTaskDep(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Create tasks
	cli.RunWithStdin(t, `{"id":"task-1","content":"Task 1"}`, "task", "create").MustSucceed(t)
	cli.RunWithStdin(t, `{"id":"task-2","content":"Task 2"}`, "task", "create").MustSucceed(t)

	// Add dependency
	result := cli.RunWithStdin(t, `{"source_id":"task-2","target_id":"task-1","relation_type":"blocked_by","add":true}`, "task", "dep")
	result.MustSucceed(t)
}

func TestTaskTree(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Create tasks
	cli.RunWithStdin(t, `{"id":"task-1","content":"Root"}`, "task", "create").MustSucceed(t)
	cli.RunWithStdin(t, `{"id":"task-2","content":"Child"}`, "task", "create").MustSucceed(t)
	cli.RunWithStdin(t, `{"source_id":"task-2","target_id":"task-1","relation_type":"blocked_by","add":true}`, "task", "dep").MustSucceed(t)

	// Show tree
	result := cli.RunWithStdin(t, `{"goal_id":"task-1"}`, "task", "tree")
	result.MustSucceed(t)
}

func TestTaskExecutable(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Create task
	cli.RunWithStdin(t, `{"id":"task-1","content":"Run tests"}`, "task", "create").MustSucceed(t)

	// Get executable tasks
	result := cli.RunWithStdin(t, `{}`, "task", "executable")
	result.MustSucceed(t)
}

func TestTaskRollback(t *testing.T) {
	dir, _ := helpers.TempWorkspace(t)
	helpers.WriteConfigForCLI(t, dir, helpers.TaskConfig(helpers.DBPath(dir)))
	cli := helpers.NewCLI(helpers.BinaryPath(t), dir)

	// Create tasks
	cli.RunWithStdin(t, `{"id":"task-1","content":"Main task"}`, "task", "create").MustSucceed(t)
	cli.RunWithStdin(t, `{"id":"task-2","content":"Recovery task"}`, "task", "create").MustSucceed(t)
	cli.RunWithStdin(t, `{"source_id":"task-2","target_id":"task-1","relation_type":"recovers_via","add":true}`, "task", "dep").MustSucceed(t)

	// Find rollback
	result := cli.RunWithStdin(t, `{"id":"task-1"}`, "task", "rollback")
	result.MustSucceed(t)
}
