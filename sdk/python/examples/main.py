"""Example: Hermem Python SDK usage.

Prerequisites:
    - Running Hermem server: hermem serve
    - API key (if configured): export HERMEM_API_KEY=your-key
    - Install: pip install hermem

Run: python sdk/python/examples/main.py
"""
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from hermem import Client, StoreRequest, SearchRequest, TaskCreateRequest, TaskListRequest


def main():
    base_url = os.environ.get("HERMEM_URL", "http://localhost:8420")
    api_key = os.environ.get("HERMEM_API_KEY")

    client = Client(base_url=base_url, api_key=api_key or "", timeout=30)

    # --- Memory ---
    print("=== Memory ===")

    try:
        result = client.memory.store(StoreRequest(
            id="example-1",
            category="fact",
            content="The Hermem knowledge graph supports semantic search.",
        ))
        print(f"Store: {result['status']}")
    except Exception as e:
        print(f"Store error: {e}")

    try:
        results = client.memory.search(SearchRequest(query="semantic search", limit=5))
        print(f"Search: {len(results['results'])} results")
    except Exception as e:
        print(f"Search error: {e}")

    # --- Tasks ---
    print("\n=== Tasks ===")

    try:
        task = client.task.create(TaskCreateRequest(
            content="Implement MCP server integration",
            context_ids=["example-1"],
        ))
        print(f"Task created: {task['id']}")

        tasks = client.task.list(TaskListRequest(status="pending"))
        print(f"Pending tasks: {len(tasks['tasks'])}")
    except Exception as e:
        print(f"Task error: {e}")

    # --- Graph ---
    print("\n=== Graph ===")

    try:
        components = client.graph.connected_components(min_size=2)
        print(f"Components: {len(components)}")
    except Exception as e:
        print(f"Graph error: {e}")

    # --- Admin ---
    print("\n=== Admin ===")

    try:
        health = client.admin.health()
        print(f"Health: {health['status']}")
    except Exception as e:
        print(f"Health error: {e}")

    print("\nDone!")


if __name__ == "__main__":
    main()
