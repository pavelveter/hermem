"""
Hermem memory provider for Hermes Agent.

Lightweight graph memory via CLI binary or HTTP server. Exposes 10 tools
covering the agent-usable surface of hermem's API: search, store, query
(read pipeline), edge (structural write), retrieve (graph walk),
timeline (recent entities), contradictions (conflict detection), and
task create/status/list (lifecycle hooks).

The admin / debugging surface (/admin/re-embed, /connected-components,
/communities, /health, /metrics, /query/explain, /task/rollback,
/recovery-plan, ...) is intentionally NOT exposed to the agent as tools
— those are operator concerns.
"""

from __future__ import annotations

import json
import logging
import os
import shutil
import subprocess
from typing import Any, Dict, List, Optional, Sequence, Union

from agent.memory_provider import MemoryProvider

logger = logging.getLogger(__name__)

HERMEM_BIN = "hermem"
HERMEM_URL = os.environ.get("HERMEM_URL", "")
_DEFAULT_CLI_TIMEOUT_S = 10
_DEFAULT_HTTP_TIMEOUT_S = 5


# ---------------------------------------------------------------------------
# Transport helpers
# ---------------------------------------------------------------------------


def _get_bin_path() -> str:
    """Find hermem binary path on disk.

    Lookup order: PATH (`shutil.which`) → `$HERMES_HOME/bin/hermem` →
    `~/.hermes/bin/hermem`. Returns the bare name `"hermem"` as a last
    resort so the subprocess raises FileNotFoundError on call.
    """
    if shutil.which(HERMEM_BIN):
        return HERMEM_BIN
    hermes_home = os.environ.get("HERMES_HOME", os.path.expanduser("~/.hermes"))
    bin_path = os.path.join(hermes_home, "bin", HERMEM_BIN)
    if os.path.isfile(bin_path) and os.access(bin_path, os.X_OK):
        return bin_path
    return HERMEM_BIN


def _cli_args(path: str) -> Sequence[str]:
    """Translate `memory store`-style or `task/create`-style path into the
    cobra nested command form. `path` is `/`-separated; each segment
    becomes a positional argument to the binary.
    """
    return [_get_bin_path()] + [seg for seg in path.split("/") if seg]


def _cli(path: str, data: dict) -> Optional[dict]:
    try:
        proc = subprocess.run(
            _cli_args(path),
            input=json.dumps(data),
            capture_output=True,
            text=True,
            timeout=_DEFAULT_CLI_TIMEOUT_S,
        )
        if proc.returncode != 0:
            logger.warning("hermem %s failed: %s", path, proc.stderr)
            return None
        if not proc.stdout.strip():
            return None
        return json.loads(proc.stdout)
    except FileNotFoundError:
        logger.warning("hermem binary not found")
        return None
    except subprocess.TimeoutExpired:
        logger.warning("hermem %s timed out after %ds", path, _DEFAULT_CLI_TIMEOUT_S)
        return None
    except Exception as e:
        logger.warning("hermem %s error: %s", path, e)
        return None


def _http(path: str, data: dict) -> Optional[dict]:
    try:
        import requests
        r = requests.post(f"{HERMEM_URL}{path}", json=data, timeout=_DEFAULT_HTTP_TIMEOUT_S)
        if r.status_code == 200:
            return r.json()
        if r.status_code in (400, 422):
            logger.info("hermem %s rejected: %d %s", path, r.status_code, r.text[:200])
        else:
            logger.warning("hermem %s unexpected status: %d", path, r.status_code)
    except Exception as e:
        logger.debug("hermem %s http error: %s", path, e)
    return None


def _call(path: str, data: dict) -> Optional[dict]:
    """Dispatch to HTTP if HERMEM_URL is set, else to CLI subprocess."""
    if HERMEM_URL:
        return _http(path, data)
    return _cli(path, data)


# ---------------------------------------------------------------------------
# Provider
# ---------------------------------------------------------------------------


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
        if shutil.which(HERMEM_BIN):
            return True
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

    # -- Tool surface -------------------------------------------------------

    def get_tool_schemas(self) -> List[Dict[str, Any]]:
        """JSON-schema list exposed to Hermes Agent. Three legacy tools
        (hermem_search / hermem_store / hermem_query) are preserved
        verbatim for back-compat with existing installations. Seven new
        tools expose the rest of the agent-usable surface."""
        return [
            # ----- Legacy tools (unchanged) -----
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
                "description": "Store a fact or observation in Hermem graph memory (category is validated server-side against [schema] allowed_categories)",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "content": {"type": "string", "description": "Content to store"},
                        "category": {
                            "type": "string",
                            "description": (
                                "Memory category. Free-form here — hermem.ini's [schema] "
                                "allowed_categories (e.g. world, opinion, experience, "
                                "observation, task, milestone) is the source of truth. "
                                "The server rejects unknown categories at request time."
                            ),
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
            # ----- New tools -----
            {
                "name": "hermem_edge",
                "description": "Link two existing memory entities with a typed relation",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "source_id": {"type": "string", "description": "Source entity ID"},
                        "target_id": {"type": "string", "description": "Target entity ID"},
                        "relation_type": {
                            "type": "string",
                            "description": "Relation type (e.g. 'mentions', 'causes', 'related_to')",
                            "enum": [
                                "prefers", "uses", "mentions", "related_to",
                                "part_of", "causes", "contradicts",
                                "blocked_by", "recovers_via",
                            ],
                        },
                        "auto_create": {
                            "type": "boolean",
                            "description": "If true and an endpoint is missing, auto-create a placeholder entity",
                            "default": False,
                        },
                    },
                    "required": ["source_id", "target_id", "relation_type"],
                },
            },
            {
                "name": "hermem_retrieve",
                "description": "Walk the graph from explicit seed IDs up to max_depth hops",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "seed_ids": {
                            "type": "array",
                            "items": {"type": "string"},
                            "description": "Starting entity IDs for the graph walk",
                        },
                        "max_depth": {
                            "type": "integer",
                            "description": "How many hops to expand from each seed",
                            "default": 2,
                        },
                    },
                    "required": ["seed_ids"],
                },
            },
            {
                "name": "hermem_timeline",
                "description": "Fetch the most chronologically recent memory entities",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "limit": {
                            "type": "integer",
                            "description": "How many recent entities to return",
                            "default": 50,
                        },
                    },
                },
            },
            {
                "name": "hermem_contradictions",
                "description": "List conflicting facts currently held in memory (optional ID filter)",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "id": {
                            "type": "string",
                            "description": "Optional entity ID to scope contradictions to",
                        },
                    },
                },
            },
            {
                "name": "hermem_task_create",
                "description": "Create an actionable task node in the memory graph, optionally linked to context entities",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "content": {"type": "string", "description": "Task description"},
                        "context_ids": {
                            "type": "array",
                            "items": {"type": "string"},
                            "description": "Optional entity IDs to link as blocked_by relations",
                        },
                    },
                    "required": ["content"],
                },
            },
            {
                "name": "hermem_task_status",
                "description": "Update the lifecycle status of a task entity",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "id": {"type": "string", "description": "Task entity ID"},
                        "status": {
                            "type": "string",
                            "enum": ["pending", "running", "completed", "failed"],
                            "description": "New lifecycle status",
                        },
                    },
                    "required": ["id", "status"],
                },
            },
            {
                "name": "hermem_task_list",
                "description": "List operational tasks, optionally filtered by status or goal",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "status": {
                            "type": "string",
                            "enum": ["pending", "running", "completed", "failed"],
                            "description": "Optional status filter",
                        },
                        "goal_id": {
                            "type": "string",
                            "description": "Optional goal/task ID to scope descendants",
                        },
                    },
                },
            },
        ]

    def handle_tool_call(self, tool_name: str, args: Dict[str, Any], **kwargs) -> str:
        # ----- Legacy tools -----
        if tool_name == "hermem_search":
            resp = _call("search", {
                "query": args["query"],
                "top_k": args.get("limit", 5),
            })
            return _json_result(resp, default_error="search failed")

        if tool_name == "hermem_store":
            content = args["content"]
            # NOTE: `hash()` is per-process salted. Two Python invocations
            # produce different IDs for the same content. That's OK here
            # because hermem's b2 runner dedup-merges by cosine similarity
            # at ingest time, not by hash. Within one process, repeated
            # stores of identical content DO collide on the same id and
            # therefore drop into the b2 merge path.
            resp = _call("store", {
                "id": f"mem-{hash(content) & 0xFFFFFFFF:08x}",
                "category": args.get("category", "world"),
                "content": content,
            })
            return _json_result(resp, default_error="store failed")

        if tool_name == "hermem_query":
            resp = _call("query", {"query": args["query"]})
            if resp is not None and "context" in resp:
                return json.dumps(resp)
            return json.dumps({"error": "query failed"})

        # ----- New tools -----
        if tool_name == "hermem_edge":
            req: Dict[str, Any] = {
                "source_id": args["source_id"],
                "target_id": args["target_id"],
                "relation_type": args["relation_type"],
                "auto_create": bool(args.get("auto_create", False)),
            }
            resp = _call("edge", req)
            return _json_result(resp, default_error="edge failed")

        if tool_name == "hermem_retrieve":
            req = {
                "seed_ids": list(args["seed_ids"]),
                "max_depth": int(args.get("max_depth", 2)),
            }
            resp = _call("retrieve", req)
            return _json_result(resp, default_error="retrieve failed")

        if tool_name == "hermem_timeline":
            req = {"limit": int(args.get("limit", 50))}
            resp = _call("timeline", req)
            return _json_result(resp, default_error="timeline failed")

        if tool_name == "hermem_contradictions":
            req: Dict[str, Any] = {}
            if args.get("id"):
                req["id"] = args["id"]
            resp = _call("contradictions", req)
            return _json_result(resp, default_error="contradictions lookup failed")

        if tool_name == "hermem_task_create":
            req = {"content": args["content"]}
            if args.get("context_ids"):
                req["context_ids"] = list(args["context_ids"])
            resp = _call("task/create", req)
            return _json_result(resp, default_error="task create failed")

        if tool_name == "hermem_task_status":
            req = {"id": args["id"], "status": args["status"]}
            resp = _call("task/status", req)
            return _json_result(resp, default_error="task status update failed")

        if tool_name == "hermem_task_list":
            req: Dict[str, Any] = {}
            if args.get("status"):
                req["status"] = args["status"]
            if args.get("goal_id"):
                req["goal_id"] = args["goal_id"]
            resp = _call("task/list", req)
            return _json_result(resp, default_error="task list failed")

        return json.dumps({"error": f"unknown tool: {tool_name}"})

    def shutdown(self) -> None:
        pass


def _json_result(resp: Optional[dict], default_error: str) -> str:
    """Coerce a `_call` response into the JSON-string contract expected by
    Hermes Agent. None → error envelope; otherwise pass through."""
    if resp is None:
        return json.dumps({"error": default_error})
    return json.dumps(resp)


def register(ctx) -> None:
    """Register hermem as a memory provider."""
    ctx.register_memory_provider(HermemProvider())
