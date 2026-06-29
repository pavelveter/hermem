"""Tests for the Hermem Python SDK."""

import json
import unittest
from unittest.mock import MagicMock, patch

from hermem import Client, APIError, StoreRequest, SearchRequest
from hermem.types import Entity, SearchResult, RetrievalResult


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


if __name__ == "__main__":
    unittest.main()
