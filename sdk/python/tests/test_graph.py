"""Tests for the GraphClient sub-client."""

import pytest

from hermem import Client
from hermem.types import APIError


def test_graph_verify(mock_server):
    with mock_server("GET", "/graph/verify", response_data={"issues": []}) as (base_url, _):
        got = Client(base_url).graph.verify()
    assert got.issues == []


def test_graph_contradictions(mock_server):
    payload = [{"source_id": "a", "source_content": "x", "target_id": "b", "target_content": "y"}]
    with mock_server("GET", "/contradictions", response_data=payload) as (base_url, _):
        got = Client(base_url).graph.contradictions("")
    assert len(got) == 1
    assert got[0].source_id == "a"


def test_graph_contradictions_with_id(mock_server):
    """When entity_id is supplied, the SDK appends ?id=<entity_id>."""
    with mock_server("GET", "/contradictions?id=paris", response_data=[]) as (base_url, captured):
        Client(base_url).graph.contradictions("paris")
    assert captured["path"] == "/contradictions?id=paris"


def test_graph_connected_components(mock_server):
    payload = [{"ids": ["a", "b", "c"], "size": 3, "avg_degree": 1.5}]
    with mock_server("GET", "/connected-components", response_data=payload) as (base_url, _):
        got = Client(base_url).graph.connected_components(0)  # 0 = no min_size query
    assert len(got) == 1
    assert got[0].size == 3


def test_graph_connected_components_with_min_size(mock_server):
    """When min_size > 0, the SDK appends ?min_size=N."""
    with mock_server("GET", "/connected-components?min_size=2", response_data=[]) as (base_url, captured):
        Client(base_url).graph.connected_components(2)
    assert captured["path"] == "/connected-components?min_size=2"


def test_graph_communities(mock_server):
    payload = {"communities": [], "global_modularity": 0.0, "total_communities": 0}
    with mock_server("GET", "/communities", response_data=payload) as (base_url, _):
        got = Client(base_url).graph.communities(0, 0)
    assert got["total_communities"] == 0


def test_graph_timeline(mock_server):
    payload = [{"id": "t1", "category": "world", "content": "x", "created_at": "2024-01-01T00:00:00Z"}]
    with mock_server("GET", "/timeline", response_data=payload) as (base_url, _):
        got = Client(base_url).graph.timeline(0)  # 0 = no limit query
    assert len(got) == 1
    assert got[0].id == "t1"


def test_graph_timeline_with_limit(mock_server):
    """When limit > 0, the SDK appends ?limit=N."""
    with mock_server("GET", "/timeline?limit=10", response_data=[]) as (base_url, captured):
        Client(base_url).graph.timeline(10)
    assert captured["path"] == "/timeline?limit=10"


def test_graph_provenance(mock_server):
    payload = [{"id": "e1", "category": "world", "content": "x", "archived": False}]
    with mock_server("GET", "/provenance", response_data=payload) as (base_url, _):
        got = Client(base_url).graph.provenance("", "", "", 0)
    assert len(got) == 1
    assert got[0].id == "e1"


def test_graph_recovery_plan(mock_server):
    payload = [{"id": "rb-1", "category": "task", "content": "recover", "archived": False}]
    with mock_server("GET", "/recovery-plan?id=task-1", response_data=payload) as (base_url, _):
        got = Client(base_url).graph.recovery_plan("task-1")
    assert len(got) == 1
    assert got[0].id == "rb-1"


def test_graph_error_propagation(mock_server):
    with mock_server("GET", "/graph/verify", response_data={"error": "db down"}, status=500) as (base_url, _):
        with pytest.raises(APIError) as exc:
            Client(base_url).graph.verify()
    assert exc.value.status_code == 500
    assert "db down" in str(exc.value)
