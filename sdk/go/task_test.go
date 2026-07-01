package hermem

import (
	"context"
	"net/http"
	"testing"
)

func TestTaskCreate(t *testing.T) {
	runSDKCall(t, "POST", "/task/create", http.StatusOK, TaskCreateResponse{ID: "task-1", Status: "pending"}, func(c *Client) {
		got, err := c.Task.Create(context.Background(), &TaskCreateRequest{Content: "do thing"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if got.ID != "task-1" {
			t.Errorf("id: got %q, want task-1", got.ID)
		}
		if got.Status != "pending" {
			t.Errorf("status: got %q, want pending", got.Status)
		}
	})
}

func TestTaskStatus(t *testing.T) {
	runSDKCall(t, "POST", "/task/status", http.StatusNoContent, nil, func(c *Client) {
		err := c.Task.Status(context.Background(), &TaskStatusRequest{ID: "task-1", Status: "done"})
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
	})
}

func TestTaskList(t *testing.T) {
	runSDKCall(t, "POST", "/task/list", http.StatusOK, TaskExecutableResponse{
		Tasks: []Entity{{ID: "t1", Category: "task", Content: "x"}},
	}, func(c *Client) {
		got, err := c.Task.List(context.Background(), &TaskListRequest{Status: "pending"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got.Tasks) != 1 {
			t.Fatalf("got %d tasks, want 1", len(got.Tasks))
		}
		if got.Tasks[0].ID != "t1" {
			t.Errorf("id: got %q, want t1", got.Tasks[0].ID)
		}
	})
}

func TestTaskShow(t *testing.T) {
	runSDKCall(t, "POST", "/task/show", http.StatusOK, TaskShowResponse{
		Entity: Entity{ID: "t1", Category: "task", Content: "x"},
	}, func(c *Client) {
		got, err := c.Task.Show(context.Background(), &TaskShowRequest{ID: "t1"})
		if err != nil {
			t.Fatalf("Show: %v", err)
		}
		if got.Entity.ID != "t1" {
			t.Errorf("id: got %q, want t1", got.Entity.ID)
		}
		if got.BlockedBy != nil {
			t.Errorf("blocked_by: got %v, want nil", got.BlockedBy)
		}
	})
}

func TestTaskDep(t *testing.T) {
	runSDKCall(t, "POST", "/task/dep", http.StatusNoContent, nil, func(c *Client) {
		err := c.Task.Dep(context.Background(), &TaskDepRequest{SourceID: "a", TargetID: "b", Add: true})
		if err != nil {
			t.Fatalf("Dep: %v", err)
		}
	})
}

func TestTaskTree(t *testing.T) {
	runSDKCall(t, "POST", "/task/tree", http.StatusOK, TaskTreeResponse{Tree: "root\n  t1\n  t2"}, func(c *Client) {
		got, err := c.Task.Tree(context.Background(), &TaskTreeRequest{GoalID: "g1"})
		if err != nil {
			t.Fatalf("Tree: %v", err)
		}
		if got.Tree == "" {
			t.Fatal("got empty tree")
		}
	})
}

func TestTaskRollback(t *testing.T) {
	runSDKCall(t, "POST", "/task/rollback", http.StatusOK, TaskRollbackResponse{RollbackTaskID: "rb-1"}, func(c *Client) {
		got, err := c.Task.Rollback(context.Background(), &TaskRollbackRequest{ID: "t1"})
		if err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		if got.RollbackTaskID != "rb-1" {
			t.Errorf("rollback_task_id: got %q, want rb-1", got.RollbackTaskID)
		}
	})
}

func TestTaskExecutable(t *testing.T) {
	runSDKCall(t, "POST", "/task/executable", http.StatusOK, TaskExecutableResponse{
		Tasks: []Entity{{ID: "t1", Category: "task", Content: "x"}},
	}, func(c *Client) {
		got, err := c.Task.Executable(context.Background(), &TaskListRequest{})
		if err != nil {
			t.Fatalf("Executable: %v", err)
		}
		if len(got.Tasks) != 1 {
			t.Fatalf("got %d tasks, want 1", len(got.Tasks))
		}
	})
}

// TestTaskNext verifies that Next() is an alias for Executable() and
// hits the same /task/executable endpoint.
func TestTaskNext(t *testing.T) {
	runSDKCall(t, "POST", "/task/executable", http.StatusOK, TaskExecutableResponse{Tasks: nil}, func(c *Client) {
		got, err := c.Task.Next(context.Background(), &TaskListRequest{})
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if got == nil {
			t.Fatal("got nil response")
		}
	})
}

func TestTaskError(t *testing.T) {
	runSDKCall(t, "POST", "/task/create", http.StatusForbidden, `{"error":"forbidden","code":"forbidden"}`, func(c *Client) {
		_, err := c.Task.Create(context.Background(), &TaskCreateRequest{Content: "x"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		apiErr, ok := err.(*APIError)
		if !ok {
			t.Fatalf("got %T, want *APIError", err)
		}
		if apiErr.StatusCode != 403 {
			t.Errorf("status: got %d, want 403", apiErr.StatusCode)
		}
		if apiErr.Code != "forbidden" {
			t.Errorf("code: got %q, want forbidden", apiErr.Code)
		}
	})
}
