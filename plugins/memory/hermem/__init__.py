"""
Hermem memory provider for Hermes Agent.

Lightweight graph memory via CLI binary or HTTP server.
"""

from __future__ import annotations

import json
import logging
import os
import subprocess
from typing import Any, Dict, List, Optional

from agent.memory_provider import MemoryProvider

logger = logging.getLogger(__name__)

HERMEM_BIN = "hermem"
HERMEM_URL = os.environ.get("HERMEM_URL", "")


def _get_bin_path() -> str:
    """Find hermem binary path."""
    import shutil
    if shutil.which(HERMEM_BIN):
        return HERMEM_BIN
    hermes_home = os.environ.get("HERMES_HOME", os.path.expanduser("~/.hermes"))
    bin_path = os.path.join(hermes_home, "bin", HERMEM_BIN)
    if os.path.isfile(bin_path) and os.access(bin_path, os.X_OK):
        return bin_path
    return HERMEM_BIN


def _cli(command: str, data: dict) -> dict | None:
    try:
        proc = subprocess.run(
            [_get_bin_path(), command],
            input=json.dumps(data),
            capture_output=True,
            text=True,
            timeout=10,
        )
        if proc.returncode != 0:
            logger.warning("hermem %s failed: %s", command, proc.stderr)
            return None
        return json.loads(proc.stdout)
    except FileNotFoundError:
        logger.warning("hermem binary not found")
        return None
    except Exception as e:
        logger.warning("hermem %s error: %s", command, e)
        return None


def _http(path: str, data: dict) -> dict | None:
    try:
        import requests
        r = requests.post(f"{HERMEM_URL}{path}", json=data, timeout=5)
        if r.status_code == 200:
            return r.json()
    except Exception:
        pass
    return None


def _call(command: str, data: dict) -> dict | None:
    if HERMEM_URL:
        return _http(f"/{command}", data)
    return _cli(command, data)


class HermemProvider(MemoryProvider):
    """Hermem graph memory provider."""

    @property
    def name(self) -> str:
        return "hermem"

    def is_available(self) -> bool:
        if HERMEM_URL:
            try:
                import requests
                r = requests.get(f"{HERMEM_URL}/health", timeout=2)
                return r.status_code == 200
            except Exception:
                return False
        # Check multiple locations for the binary
        import shutil
        if shutil.which(HERMEM_BIN):
            return True
        # Check ~/.hermes/bin/
        hermes_home = os.environ.get("HERMES_HOME", os.path.expanduser("~/.hermes"))
        bin_path = os.path.join(hermes_home, "bin", HERMEM_BIN)
        if os.path.isfile(bin_path) and os.access(bin_path, os.X_OK):
            return True
        return False

    def initialize(self, session_id: str, **kwargs) -> None:
        pass

    def prefetch(self, query: str, *, session_id: str = "") -> str:
        resp = _call("query", {"query": query})
        if not resp or "context" not in resp:
            return ""
        return resp["context"]

    def sync_turn(
        self,
        user_content: str,
        assistant_content: str,
        *,
        session_id: str = "",
        messages: Optional[List[Dict[str, Any]]] = None,
    ) -> None:
        dialog = f"User: {user_content}\nAssistant: {assistant_content}"
        _call("ingest", {"dialog": dialog})

    def get_tool_schemas(self) -> List[Dict[str, Any]]:
        return [
            {
                "name": "hermem_search",
                "description": "Search Hermem graph memory for relevant facts and entities",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {"type": "string", "description": "Search query"},
                        "limit": {"type": "integer", "description": "Max results", "default": 5},
                    },
                    "required": ["query"],
                },
            },
            {
                "name": "hermem_store",
                "description": "Store a fact or observation in Hermem graph memory",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "content": {"type": "string", "description": "Content to store"},
                        "category": {
                            "type": "string",
                            "enum": ["world", "opinion", "experience", "observation"],
                            "description": "Memory category",
                        },
                    },
                    "required": ["content"],
                },
            },
            {
                "name": "hermem_query",
                "description": "Full pipeline: search + graph walk + format as markdown context",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {"type": "string", "description": "User query to find context for"},
                    },
                    "required": ["query"],
                },
            },
        ]

    def handle_tool_call(self, tool_name: str, args: Dict[str, Any], **kwargs) -> str:
        if tool_name == "hermem_search":
            resp = _call("search", {"query": args["query"], "top_k": args.get("limit", 5)})
            if not resp:
                return json.dumps({"error": "search failed"})
            return json.dumps(resp)

        elif tool_name == "hermem_store":
            cat = args.get("category", "world")
            resp = _call("store", {
                "id": f"mem-{hash(args['content']) & 0xFFFFFFFF:08x}",
                "category": cat,
                "content": args["content"],
            })
            if resp and resp.get("status") == "ok":
                return json.dumps({"status": "ok", "category": cat})
            return json.dumps({"error": "store failed"})

        elif tool_name == "hermem_query":
            resp = _call("query", {"query": args["query"]})
            if resp and "context" in resp:
                return json.dumps({"context": resp["context"]})
            return json.dumps({"error": "query failed"})

        return json.dumps({"error": f"unknown tool: {tool_name}"})

    def shutdown(self) -> None:
        pass


def register(ctx) -> None:
    """Register hermem as a memory provider."""
    ctx.register_memory_provider(HermemProvider())
