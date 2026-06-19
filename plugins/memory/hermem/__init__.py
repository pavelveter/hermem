"""
Hermem memory provider for Hermes Agent.

Connects to a running Hermem server (Go) via HTTP API.
Provides graph-based memory with vector search and entity deduplication.
"""

import json
import os
from typing import Any

try:
    import requests
except ImportError:
    requests = None


PROVIDER_NAME = "hermem"

DEFAULT_URL = "http://localhost:8420"


def _get_url() -> str:
    return os.environ.get("HERMEM_URL", DEFAULT_URL)


def _post(path: str, data: dict) -> dict | None:
    if requests is None:
        return None
    try:
        r = requests.post(f"{_get_url()}{path}", json=data, timeout=5)
        if r.status_code == 200:
            return r.json()
    except Exception:
        pass
    return None


# --- Hermes memory provider interface ---


def prefetch(query: str, limit: int = 10) -> str:
    """Search memory and return context for injection into system prompt."""
    resp = _post("/query", {"query": query, "top_k": limit})
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
    _post("/ingest", {"dialog": dialog})


def store(content: str, category: str = "world", entry_id: str = "", **kwargs) -> dict:
    """Store a single memory entry."""
    data = {
        "id": entry_id or f"mem-{hash(content) & 0xFFFFFFFF:08x}",
        "category": category,
        "content": content,
    }
    return _post("/store", data) or {"error": "unreachable"}


def search(query: str, limit: int = 10) -> list[dict]:
    """Search memory by vector similarity."""
    resp = _post("/search", {"query": query, "top_k": limit})
    if not resp:
        return []
    if isinstance(resp, list):
        return resp
    return []


def recall(limit: int = 10, min_importance: int = 0) -> list[dict]:
    """Recall recent or important memories."""
    return []


def health() -> bool:
    """Check if Hermem server is reachable."""
    if requests is None:
        return False
    try:
        r = requests.get(f"{_get_url()}/health", timeout=2)
        return r.status_code == 200
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
        resp = _post("/query", {"query": arguments["query"]})
        if resp and "context" in resp:
            return resp["context"] or "No context found."
        return "Error: Hermem server unreachable."

    return f"Unknown tool: {name}"
