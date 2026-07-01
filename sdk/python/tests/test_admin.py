"""Tests for the AdminClient sub-client."""

import pytest

from hermem import Client
from hermem.types import APIError


def test_admin_health(mock_server):
    with mock_server("GET", "/health", response_data={"status": "ok"}) as (base_url, _):
        got = Client(base_url).admin.health()
    assert got.status == "ok"


def test_admin_ready(mock_server):
    payload = {"status": "ready", "latency_ms": 12, "checks": None}
    with mock_server("GET", "/health/ready", response_data=payload) as (base_url, _):
        got = Client(base_url).admin.ready()
    assert got.status == "ready"
    assert got.latency_ms == 12


def test_admin_error_propagation(mock_server):
    with mock_server("GET", "/health", response_data={"error": "service down"}, status=503) as (base_url, _):
        with pytest.raises(APIError) as exc:
            Client(base_url).admin.health()
    assert exc.value.status_code == 503
    assert "service down" in str(exc.value)
