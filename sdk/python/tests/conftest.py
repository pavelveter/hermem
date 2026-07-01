"""Pytest fixtures for the Hermem Python SDK tests."""

import json
import threading
from contextlib import contextmanager
from http.server import HTTPServer, BaseHTTPRequestHandler

import pytest


@pytest.fixture
def mock_server():
    """Yields a factory for HTTP mock servers.

    Usage::

        def test_x(mock_server):
            with mock_server("POST", "/store", status=204) as (base_url, captured):
                client = Client(base_url)
                client.memory.store(StoreRequest(id="x", category="y", content="z"))
            assert captured["method"] == "POST"
            assert captured["path"] == "/store"
    """

    @contextmanager
    def _factory(expected_method: str, expected_path: str, response_data=None, status: int = 200):
        captured = {"method": None, "path": None, "body": b""}

        class _Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self._handle("GET")

            def do_POST(self):
                self._handle("POST")

            def _handle(self, method):
                captured["method"] = method
                captured["path"] = self.path
                content_length = int(self.headers.get("Content-Length", 0))
                if content_length > 0:
                    captured["body"] = self.rfile.read(content_length)
                self.send_response(status)
                if response_data is not None:
                    self.send_header("Content-Type", "application/json")
                self.end_headers()
                if response_data is not None:
                    self.wfile.write(json.dumps(response_data).encode())

            def log_message(self, format, *args):
                pass  # silence request logging

        srv = HTTPServer(("127.0.0.1", 0), _Handler)
        thread = threading.Thread(target=srv.serve_forever, daemon=True)
        thread.start()
        try:
            yield f"http://127.0.0.1:{srv.server_address[1]}", captured
        finally:
            srv.shutdown()

    yield _factory
