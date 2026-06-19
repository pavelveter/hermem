"""
Hermem memory provider for Hermes Agent.

Supports two modes:
  - CLI (default): calls `hermem` binary directly, no server needed
  - Server: connects to running Hermem server via HTTP
"""

import json
import os
import subprocess
from typing import Any


PROVIDER_NAME = "hermem"

DEFAULT_URL = "http://localhost:8420"
HERMEM_BIN = os.environ.get("HERMEM_BIN", "hermem")
HERMEM_URL = os.environ.get("HERMEM_URL", "")

USE_SERVER = bool(HERMEM_URL)


def _get_url() -> str:
    return HERMEM_URL or DEFAULT_URL


def _cli(command: str, data: dict) -> dict | None:
    """Call hermem binary via CLI."""
    try:
        proc = subprocess.run(
            [HERMEM_BIN, command],
            input=json.dumps(data),
            capture_output=True,
            text=True,
            timeout=10,
        )
        if proc.returncode != 0:
            return None
        return json.loads(proc.stdout)
    except Exception:
        return None


def _post(path: str, data: dict) -> dict | None:
    """Call hermem server via HTTP."""
    try:
        import requests
        r = requests.post(f"{_get_url()}{path}", json=data, timeout=5)
        if r.status_code == 200:
            return r.json()
    except Exception:
        pass
    return None


def _call(command: str, data: dict) -> dict | None:
    if USE_SERVER:
        return _post(f"/{command}", data)
    return _cli(command, data)


# --- Hermes memory provider interface ---


def prefetch(query: str, limit: int = 10) -> str:
    """Search memory and return context for injection into system prompt."""
    resp = _call("query", {"query": query})
    if not resp or "context" not in resp:
        return ""
    return resp["context"]


def sync_turn(session_id: str, messages: list[dict]) -> None:
    """Persist conversation turn to memory (background, non-blocking)."""
    dialog = "\n".join(
        f"{m.get('role', 'user')}: {m.get('content', '')}" for m in messages
    )
    if not dialog.strip():
        return
    _call("ingest", {"dialog": dialog})


def store(content: str, category: str = "world", entry_id: str = "", **kwargs) -> dict:
    """Store a single memory entry."""
    data = {
        "id": entry_id or f"mem-{hash(content) & 0xFFFFFFFF:08x}",
        "category": category,
        "content": content,
    }
    return _call("store", data) or {"error": "unreachable"}


def search(query: str, limit: int = 10) -> list[dict]:
    """Search memory by vector similarity."""
    resp = _call("search", {"query": query, "top_k": limit})
    if not resp:
        return []
    if isinstance(resp, list):
        return resp
    return []


def recall(limit: int = 10, min_importance: int = 0) -> list[dict]:
    """Recall recent or important memories."""
    return []


def health() -> bool:
    """Check if Hermem is reachable."""
    if USE_SERVER:
        try:
            import requests
            r = requests.get(f"{_get_url()}/health", timeout=2)
            return r.status_code == 200
        except Exception:
            return False
    # CLI mode: check binary exists
    try:
        proc = subprocess.run([HERMEM_BIN], capture_output=True, timeout=2)
        return proc.returncode == 1  # exit 1 = no command, binary exists
    except Exception:
        return False


# --- Tool definitions for Hermes ---


TOOLS = [
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


def handle_tool(name: str, arguments: dict) -> str:
    """Handle tool calls from Hermes agent."""
    if name == "hermem_search":
        results = search(arguments["query"], arguments.get("limit", 5))
        if not results:
            return "No results found."
        lines = []
        for r in results:
            entity = r.get("Entity", r)
            sim = r.get("Similarity", 0)
            lines.append(f"- [{entity.get('ID', '?')}] (sim: {sim:.3f}) {entity.get('Content', '')}")
        return "\n".join(lines)

    elif name == "hermem_store":
        cat = arguments.get("category", "world")
        resp = store(arguments["content"], category=cat)
        if resp and resp.get("status") == "ok":
            return f"Stored in {cat} category."
        return f"Error: {resp}"

    elif name == "hermem_query":
        resp = _call("query", {"query": arguments["query"]})
        if resp and "context" in resp:
            return resp["context"] or "No context found."
        return "Error: Hermem unreachable."

    return f"Unknown tool: {name}"
