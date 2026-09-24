import importlib.util
import sys
import types
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock


MODULE_PATH = Path(__file__).parents[1] / "plugins" / "memory" / "hermem" / "__init__.py"


def load_provider_module():
    agent_module = types.ModuleType("agent")
    memory_provider_module = types.ModuleType("agent.memory_provider")

    class MemoryProvider:
        pass

    memory_provider_module.MemoryProvider = MemoryProvider
    agent_module.memory_provider = memory_provider_module

    previous_modules = {
        name: sys.modules.get(name)
        for name in ("agent", "agent.memory_provider")
    }
    sys.modules["agent"] = agent_module
    sys.modules["agent.memory_provider"] = memory_provider_module
    try:
        spec = importlib.util.spec_from_file_location("hermem_provider_test", MODULE_PATH)
        if spec is None or spec.loader is None:
            raise RuntimeError(f"could not load {MODULE_PATH}")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module
    finally:
        for name, previous in previous_modules.items():
            if previous is None:
                sys.modules.pop(name, None)
            else:
                sys.modules[name] = previous


class ProviderCLIPathsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.provider = load_provider_module()
        cls.original_get_bin_path = cls.provider._get_bin_path
        cls.provider._get_bin_path = lambda: "hermem"

    @classmethod
    def tearDownClass(cls):
        cls.provider._get_bin_path = cls.original_get_bin_path

    def test_maps_memory_and_graph_paths_to_grouped_cli_commands(self):
        expected = {
            "query": ["memory", "query"],
            "ingest": ["memory", "ingest"],
            "search": ["memory", "search"],
            "store": ["memory", "store"],
            "edge": ["memory", "edge"],
            "retrieve": ["memory", "retrieve"],
            "timeline": ["time", "timeline"],
            "contradictions": ["graph", "contradictions"],
        }

        for path, command in expected.items():
            with self.subTest(path=path):
                self.assertEqual(
                    self.provider._cli_args(path),
                    ["hermem", *command],
                )

    def test_preserves_already_grouped_task_paths(self):
        self.assertEqual(
            self.provider._cli_args("task/create"),
            ["hermem", "task", "create"],
        )
        self.assertEqual(
            self.provider._cli_args("task/status"),
            ["hermem", "task", "status"],
        )
        self.assertEqual(
            self.provider._cli_args("task/list"),
            ["hermem", "task", "list"],
        )

    def test_normalizes_text_only_cli_outputs(self):
        completed = SimpleNamespace(
            returncode=0,
            stdout="first\nsecond\nthird\n",
            stderr="",
        )
        with mock.patch.object(
            self.provider.subprocess,
            "run",
            return_value=completed,
        ) as run:
            result = self.provider._cli("timeline", {"limit": 100})

        self.assertEqual(result, {"text": "first\nsecond\nthird"})
        self.assertEqual(
            run.call_args.args[0],
            ["hermem", "time", "timeline", "--limit", "100"],
        )

    def test_passes_contradiction_id_as_cli_argument(self):
        completed = SimpleNamespace(
            returncode=0,
            stdout="[a] first\n  contradicts [b] second\n",
            stderr="",
        )
        with mock.patch.object(
            self.provider.subprocess,
            "run",
            return_value=completed,
        ) as run:
            result = self.provider._cli("contradictions", {"id": "a"})

        self.assertIn("contradicts", result["text"])
        self.assertEqual(
            run.call_args.args[0],
            ["hermem", "graph", "contradictions", "a"],
        )

    def test_uses_get_and_query_for_read_only_http_routes(self):
        response = SimpleNamespace(
            status_code=200,
            content=b"[]",
            text="[]",
            json=lambda: [],
        )
        requests = SimpleNamespace(get=mock.Mock(return_value=response))
        original_url = self.provider.HERMEM_URL
        self.provider.HERMEM_URL = "http://hermem.test"
        try:
            with mock.patch.dict(sys.modules, {"requests": requests}):
                result = self.provider._http("timeline", {"limit": 7})
        finally:
            self.provider.HERMEM_URL = original_url

        self.assertEqual(result, [])
        requests.get.assert_called_once_with(
            "http://hermem.test/timeline",
            params={"limit": 7},
            timeout=self.provider._DEFAULT_HTTP_TIMEOUT_S,
        )

    def test_accepts_empty_http_success_responses(self):
        response = SimpleNamespace(
            status_code=204,
            content=b"",
            text="",
            json=mock.Mock(side_effect=ValueError("empty body")),
        )
        requests = SimpleNamespace(post=mock.Mock(return_value=response))
        original_url = self.provider.HERMEM_URL
        self.provider.HERMEM_URL = "http://hermem.test"
        try:
            with mock.patch.dict(sys.modules, {"requests": requests}):
                result = self.provider._http("task/status", {"id": "task-1"})
        finally:
            self.provider.HERMEM_URL = original_url

        self.assertEqual(result, {})
        requests.post.assert_called_once_with(
            "http://hermem.test/task/status",
            json={"id": "task-1"},
            timeout=self.provider._DEFAULT_HTTP_TIMEOUT_S,
        )

    def test_forwards_contradiction_filter_to_http_query(self):
        response = SimpleNamespace(
            status_code=200,
            content=b"[]",
            text="[]",
            json=lambda: [],
        )
        requests = SimpleNamespace(get=mock.Mock(return_value=response))
        original_url = self.provider.HERMEM_URL
        self.provider.HERMEM_URL = "http://hermem.test"
        try:
            with mock.patch.dict(sys.modules, {"requests": requests}):
                result = self.provider._http("contradictions", {"id": "entity-1"})
        finally:
            self.provider.HERMEM_URL = original_url

        self.assertEqual(result, [])
        requests.get.assert_called_once_with(
            "http://hermem.test/contradictions",
            params={"id": "entity-1"},
            timeout=self.provider._DEFAULT_HTTP_TIMEOUT_S,
        )

    def test_health_check_normalizes_trailing_slash(self):
        response = SimpleNamespace(status_code=200)
        requests = SimpleNamespace(get=mock.Mock(return_value=response))
        original_url = self.provider.HERMEM_URL
        self.provider.HERMEM_URL = "http://hermem.test/"
        try:
            with mock.patch.dict(sys.modules, {"requests": requests}):
                available = self.provider.HermemProvider().is_available()
        finally:
            self.provider.HERMEM_URL = original_url

        self.assertTrue(available)
        requests.get.assert_called_once_with(
            "http://hermem.test/health",
            timeout=2,
        )


if __name__ == "__main__":
    unittest.main()
