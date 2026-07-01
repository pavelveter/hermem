"""Tests for the MemoryClient sub-client."""

import json

import pytest

from hermem import Client, StoreRequest, SearchRequest, IngestRequest, EdgeRequest, RetrieveRequest, ReEmbedRequest
from hermem.types import APIError


def test_memory_store(mock_server):
    with mock_server("POST", "/store", status=204) as (base_url, captured):
        Client(base_url).memory.store(StoreRequest(id="paris", category="world", content="Paris is the capital of France"))
    assert captured["method"] == "POST"
    assert captured["path"] == "/store"
    body = json.loads(captured["body"])
    assert body == {"id": "paris", "category": "world", "content": "Paris is the capital of France"}


def test_memory_search(mock_server):
    payload = [{"entity": {"id": "paris", "category": "world", "content": "x", "archived": False}, "similarity": 0.95}]
    with mock_server("POST", "/search", response_data=payload) as (base_url, _):
        got = Client(base_url).memory.search(SearchRequest(query="capital", top_k=5))
    assert len(got) == 1
    assert got[0].entity.id == "paris"
    assert got[0].similarity == pytest.approx(0.95)


def test_memory_retrieve(mock_server):
    payload = {"seed_nodes": [{"entity": {"id": "paris", "category": "world", "content": "x", "archived": False}}]}
    with mock_server("POST", "/retrieve", response_data=payload) as (base_url, _):
        got = Client(base_url).memory.retrieve(RetrieveRequest(seed_ids=["paris"], max_depth=2))
    assert len(got.seed_nodes) == 1
    assert got.seed_nodes[0].entity.id == "paris"


def test_memory_query(mock_server):
    with mock_server("POST", "/query", response_data={"context": "Paris is the capital of France."}) as (base_url, _):
        got = Client(base_url).memory.query(SearchRequest(query="capital", top_k=5))
    assert got.context == "Paris is the capital of France."


def test_memory_explain(mock_server):
    payload = {"seed_nodes": [{"entity": {"id": "paris", "category": "world", "content": "x", "archived": False}}]}
    with mock_server("POST", "/query/explain", response_data=payload) as (base_url, _):
        got = Client(base_url).memory.explain(SearchRequest(query="capital", top_k=5))
    assert len(got.seed_nodes) == 1
    assert got.seed_nodes[0].entity.id == "paris"


def test_memory_ingest(mock_server):
    with mock_server("POST", "/ingest", status=204) as (base_url, captured):
        Client(base_url).memory.ingest(IngestRequest(dialog="I love Paris."))
    body = json.loads(captured["body"])
    assert body == {"dialog": "I love Paris."}


def test_memory_edge(mock_server):
    with mock_server("POST", "/edge", status=204) as (base_url, captured):
        Client(base_url).memory.edge(EdgeRequest(
            source_id="a", target_id="b", relation_type="knows", auto_create=True,
        ))
    body = json.loads(captured["body"])
    assert body == {"source_id": "a", "target_id": "b", "relation_type": "knows", "auto_create": True}


def test_memory_re_embed(mock_server):
    payload = {"total_entities": 100, "re_embedded": 100, "skipped": 0, "failed": 0, "elapsed": "1.0s", "old_dim": 384, "new_dim": 384, "batches": 1}
    with mock_server("POST", "/admin/re-embed", response_data=payload) as (base_url, _):
        got = Client(base_url).memory.re_embed(ReEmbedRequest(dim=384))
    assert got.total_entities == 100
    assert got.old_dim == 384
    assert got.new_dim == 384


def test_memory_error_propagation(mock_server):
    with mock_server(
        "POST", "/store",
        response_data={"error": "validation failed", "code": "invalid"},
        status=400,
    ) as (base_url, _):
        with pytest.raises(APIError) as exc:
            Client(base_url).memory.store(StoreRequest(id="x", category="y", content="z"))
    assert exc.value.status_code == 400
    assert exc.value.code == "invalid"
    assert "validation failed" in str(exc.value)
