"""Tests for the TaskClient sub-client."""

import pytest

from hermem import Client, TaskCreateRequest, TaskStatusRequest, TaskListRequest, TaskShowRequest, TaskDepRequest, TaskTreeRequest, TaskRollbackRequest
from hermem.types import APIError


def test_task_create(mock_server):
    with mock_server("POST", "/task/create", response_data={"id": "task-1", "status": "pending"}) as (base_url, _):
        got = Client(base_url).task.create(TaskCreateRequest(content="do thing"))
    assert got.id == "task-1"
    assert got.status == "pending"


def test_task_status(mock_server):
    with mock_server("POST", "/task/status", status=204) as (base_url, _):
        Client(base_url).task.status(TaskStatusRequest(id="task-1", status="done"))


def test_task_list(mock_server):
    payload = {"tasks": [{"id": "t1", "category": "task", "content": "x", "archived": False}]}
    with mock_server("POST", "/task/list", response_data=payload) as (base_url, _):
        got = Client(base_url).task.list_tasks(TaskListRequest(status="pending"))
    assert len(got.tasks) == 1
    assert got.tasks[0].id == "t1"


def test_task_show(mock_server):
    payload = {"entity": {"id": "t1", "category": "task", "content": "x", "archived": False}}
    with mock_server("POST", "/task/show", response_data=payload) as (base_url, _):
        got = Client(base_url).task.show(TaskShowRequest(id="t1"))
    assert got.entity.id == "t1"
    assert got.blocked_by == []


def test_task_dep(mock_server):
    with mock_server("POST", "/task/dep", status=204) as (base_url, _):
        Client(base_url).task.dep(TaskDepRequest(source_id="a", target_id="b", add=True))


def test_task_tree(mock_server):
    with mock_server("POST", "/task/tree", response_data={"tree": "root\n  t1\n  t2"}) as (base_url, _):
        got = Client(base_url).task.tree(TaskTreeRequest(goal_id="g1"))
    assert "root" in got.tree
    assert "t1" in got.tree


def test_task_rollback(mock_server):
    with mock_server("POST", "/task/rollback", response_data={"rollback_task_id": "rb-1"}) as (base_url, _):
        got = Client(base_url).task.rollback(TaskRollbackRequest(id="t1"))
    assert got.rollback_task_id == "rb-1"


def test_task_executable(mock_server):
    payload = {"tasks": [{"id": "t1", "category": "task", "content": "x", "archived": False}]}
    with mock_server("POST", "/task/executable", response_data=payload) as (base_url, _):
        got = Client(base_url).task.executable(TaskListRequest())
    assert len(got.tasks) == 1


def test_task_next(mock_server):
    """TestClient.Next is an alias for Executable and hits /task/executable."""
    with mock_server("POST", "/task/executable", response_data={"tasks": []}) as (base_url, _):
        got = Client(base_url).task.next(TaskListRequest())
    assert got.tasks == []


def test_task_error_propagation(mock_server):
    with mock_server(
        "POST", "/task/create",
        response_data={"error": "forbidden", "code": "forbidden"},
        status=403,
    ) as (base_url, _):
        with pytest.raises(APIError) as exc:
            Client(base_url).task.create(TaskCreateRequest(content="x"))
    assert exc.value.status_code == 403
    assert exc.value.code == "forbidden"
