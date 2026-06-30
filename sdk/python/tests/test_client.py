"""Tests for the Hermem Python SDK."""

import json
import threading
import unittest
from http.server import HTTPServer, BaseHTTPRequestHandler
from unittest.mock import MagicMock, patch

from hermem import Client, APIError, StoreRequest, SearchRequest
from hermem.client import SDK_VERSION, _parse_major
from hermem.types import Entity, SearchResult, RetrievalResult


class _VersionHandler(BaseHTTPRequestHandler):
    """HTTP handler that returns a configurable X-Hermem-API-Version."""

    server_version = "0.1.0"

    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("X-Hermem-API-Version", self.server_version)
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')

    def log_message(self, format, *args):
        pass  # suppress logs


def _start_server(version="0.1.0"):
    _VersionHandler.server_version = version
    srv = HTTPServer(("127.0.0.1", 0), _VersionHandler)
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    return srv


class TestClient(unittest.TestCase):
    def test_new_client(self):
        c = Client("http://localhost:8420")
        self.assertEqual(c._base_url, "http://localhost:8420")
        self.assertIsNotNone(c.memory)
        self.assertIsNotNone(c.task)
        self.assertIsNotNone(c.graph)
        self.assertIsNotNone(c.admin)

    def test_with_api_key(self):
        c = Client("http://localhost:8420").with_api_key("test-key")
        self.assertEqual(c._api_key, "test-key")

    def test_strips_trailing_slash(self):
        c = Client("http://localhost:8420/")
        self.assertEqual(c._base_url, "http://localhost:8420")


class TestTypes(unittest.TestCase):
    def test_entity(self):
        e = Entity(id="test", category="world", content="hello")
        self.assertEqual(e.id, "test")
        self.assertFalse(e.archived)

    def test_store_request(self):
        req = StoreRequest(id="test", category="world", content="hello")
        self.assertEqual(req.id, "test")

    def test_search_request(self):
        req = SearchRequest(query="test")
        self.assertEqual(req.top_k, 5)

    def test_api_error(self):
        err = APIError(status_code=404, message="not found", code="not_found")
        self.assertEqual(err.status_code, 404)
        self.assertIn("not found", str(err))


class TestAPIError(unittest.TestCase):
    def test_error_message(self):
        err = APIError(status_code=400, message="bad request")
        self.assertEqual(str(err), "hermem: bad request (status=400)")

    def test_error_with_code(self):
        err = APIError(status_code=404, message="not found", code="not_found")
        self.assertEqual(err.code, "not_found")


class TestVersionNegotiation(unittest.TestCase):
    def test_same_major_no_warning(self):
        srv = _start_server("0.5.0")
        try:
            c = Client(f"http://127.0.0.1:{srv.server_address[1]}")
            calls = []
            c._on_version_mismatch = lambda s, k: calls.append((s, k))
            c.admin.health()
            self.assertEqual(calls, [])
        finally:
            srv.shutdown()

    def test_different_major_calls_callback(self):
        srv = _start_server("1.0.0")
        try:
            c = Client(f"http://127.0.0.1:{srv.server_address[1]}")
            calls = []
            c._on_version_mismatch = lambda s, k: calls.append((s, k))
            c.admin.health()
            self.assertEqual(calls, [("1.0.0", SDK_VERSION)])
        finally:
            srv.shutdown()

    def test_strict_mode_raises(self):
        srv = _start_server("1.0.0")
        try:
            c = Client(f"http://127.0.0.1:{srv.server_address[1]}", strict=True)
            with self.assertRaises(APIError) as ctx:
                c.admin.health()
            self.assertIn("version mismatch", str(ctx.exception))
        finally:
            srv.shutdown()

    def test_custom_callback(self):
        srv = _start_server("2.0.0")
        try:
            captured = []
            c = Client(
                f"http://127.0.0.1:{srv.server_address[1]}",
                on_version_mismatch=lambda s, k: captured.append((s, k)),
            )
            c.admin.health()
            self.assertEqual(captured, [("2.0.0", SDK_VERSION)])
        finally:
            srv.shutdown()

    def test_checked_once(self):
        srv = _start_server("1.0.0")
        try:
            c = Client(f"http://127.0.0.1:{srv.server_address[1]}")
            calls = []
            c._on_version_mismatch = lambda s, k: calls.append((s, k))
            for _ in range(3):
                c.admin.health()
            self.assertEqual(len(calls), 1)
        finally:
            srv.shutdown()

    def test_no_header_skips_check(self):
        class _NoHeaderHandler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"status":"ok"}')
            def log_message(self, format, *args):
                pass

        srv = HTTPServer(("127.0.0.1", 0), _NoHeaderHandler)
        t = threading.Thread(target=srv.serve_forever, daemon=True)
        t.start()
        try:
            c = Client(f"http://127.0.0.1:{srv.server_address[1]}")
            calls = []
            c._on_version_mismatch = lambda s, k: calls.append((s, k))
            c.admin.health()
            self.assertEqual(calls, [])
        finally:
            srv.shutdown()


class TestParseMajor(unittest.TestCase):
    def test_valid(self):
        self.assertEqual(_parse_major("0.3.0"), 0)
        self.assertEqual(_parse_major("1.0.0"), 1)
        self.assertEqual(_parse_major("2.1.3"), 2)

    def test_invalid(self):
        self.assertEqual(_parse_major(""), 0)
        self.assertEqual(_parse_major("abc"), 0)


if __name__ == "__main__":
    unittest.main()
