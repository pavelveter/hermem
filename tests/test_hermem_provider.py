import importlib.util
import sys
import types
import unittest
from pathlib import Path


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


if __name__ == "__main__":
    unittest.main()
