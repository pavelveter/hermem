"""Hermem Python client — handcrafted HTTP client for the Hermem API."""

from __future__ import annotations

import json
import warnings
from typing import Any, Callable, Dict, List, Optional, Type, TypeVar
from urllib.parse import urlencode

import urllib.request
import urllib.error

from hermem.types import (
    APIError,
    ConnectedComponent,
    ContradictionPair,
    Edge,
    EdgeRequest,
    Entity,
    GraphNode,
    HealthResponse,
    IngestRequest,
    QueryResponse,
    ReEmbedRequest,
    ReEmbedResult,
    ReadyResponse,
    RetrieveRequest,
    RetrievalResult,
    RetrievedFact,
    SearchRequest,
    SearchResult,
    StoreRequest,
    TaskCreateRequest,
    TaskCreateResponse,
    TaskDepRequest,
    TaskExecutableResponse,
    TaskListRequest,
    TaskRollbackRequest,
    TaskRollbackResponse,
    TaskShowRequest,
    TaskShowResponse,
    TaskStatusRequest,
    TaskTreeResponse,
    TimelineEntry,
    VerifyReport,
)

SDK_VERSION = "0.1.0"

T = TypeVar("T")


class Client:
    """Hermem API client.

    Usage::

        client = Client("http://localhost:8420")
        client.with_api_key("your-key")

        # Store
        client.memory.store(StoreRequest(
            id="paris",
            category="world",
            content="Paris is the capital of France",
        ))

        # Search
        results = client.memory.search(SearchRequest(query="capital of France"))

    Version mismatch behavior:
        By default, a warning is emitted if the server's MAJOR version
        differs from the SDK's MAJOR version. Set ``strict=True`` to
        raise ``APIError`` instead. Provide a custom ``on_version_mismatch``
        callback to override both behaviors.
    """

    def __init__(
        self,
        base_url: str,
        api_key: str = "",
        timeout: float = 30.0,
        strict: bool = False,
        on_version_mismatch: Optional[Callable[[str, str], None]] = None,
    ):
        self._base_url = base_url.rstrip("/")
        self._api_key = api_key
        self._timeout = timeout
        self._strict = strict
        self._on_version_mismatch = on_version_mismatch
        self._version_checked = False
        self.memory = MemoryClient(self)
        self.task = TaskClient(self)
        self.graph = GraphClient(self)
        self.admin = AdminClient(self)

    def with_api_key(self, key: str) -> "Client":
        self._api_key = key
        return self

    def _do(
        self,
        method: str,
        path: str,
        body: Optional[Dict[str, Any]] = None,
        result_type: Optional[Type[T]] = None,
    ) -> Any:
        url = self._base_url + path
        data = json.dumps(body).encode() if body is not None else None

        headers = {"Content-Type": "application/json"}
        if self._api_key:
            headers["X-API-Key"] = self._api_key

        req = urllib.request.Request(url, data=data, headers=headers, method=method)

        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                self._check_version(resp)
                resp_body = resp.read().decode()
                if resp_body:
                    return json.loads(resp_body)
                return None
        except urllib.error.HTTPError as e:
            resp_body = e.read().decode() if e.fp else ""
            try:
                err_data = json.loads(resp_body)
                raise APIError(
                    status_code=e.code,
                    message=err_data.get("error", resp_body),
                    code=err_data.get("code", ""),
                    field=err_data.get("field", ""),
                )
            except (json.JSONDecodeError, KeyError):
                raise APIError(status_code=e.code, message=resp_body)

    def _check_version(self, resp: Any) -> None:
        """Check X-Hermem-API-Version header for MAJOR mismatch (once)."""
        if self._version_checked:
            return
        self._version_checked = True

        server_version = resp.headers.get("X-Hermem-API-Version", "")
        if not server_version:
            return

        server_major = _parse_major(server_version)
        sdk_major = _parse_major(SDK_VERSION)
        if server_major == sdk_major:
            return

        if self._on_version_mismatch:
            self._on_version_mismatch(server_version, SDK_VERSION)
        elif self._strict:
            raise APIError(
                status_code=0,
                message=f"version mismatch: server={server_version} sdk={SDK_VERSION}",
            )
        else:
            warnings.warn(
                f"hermem: server version {server_version} differs from "
                f"SDK version {SDK_VERSION} (MAJOR mismatch)",
                stacklevel=4,
            )

    def _get(self, path: str) -> Any:
        return self._do("GET", path)

    def _post(self, path: str, body: Optional[Dict[str, Any]] = None) -> Any:
        return self._do("POST", path, body)


class MemoryClient:
    """Memory operations (store, search, query, etc.)."""

    def __init__(self, client: Client):
        self._c = client

    def store(self, req: StoreRequest) -> None:
        body = {"id": req.id, "category": req.category, "content": req.content}
        if req.embedding is not None:
            body["embedding"] = req.embedding
        self._c._post("/store", body)

    def search(self, req: SearchRequest) -> List[SearchResult]:
        data = self._c._post("/search", {"query": req.query, "top_k": req.top_k})
        return [SearchResult(entity=Entity(**r["entity"]), similarity=r["similarity"]) for r in data]

    def retrieve(self, req: RetrieveRequest) -> RetrievalResult:
        data = self._c._post("/retrieve", {"seed_ids": req.seed_ids, "max_depth": req.max_depth})
        return _parse_retrieval_result(data)

    def query(self, req: SearchRequest) -> QueryResponse:
        data = self._c._post("/query", {"query": req.query, "top_k": req.top_k})
        return QueryResponse(context=data["context"])

    def explain(self, req: SearchRequest) -> RetrievalResult:
        data = self._c._post("/query/explain", {"query": req.query, "top_k": req.top_k})
        return _parse_retrieval_result(data)

    def ingest(self, req: IngestRequest) -> None:
        self._c._post("/ingest", {"dialog": req.dialog})

    def edge(self, req: EdgeRequest) -> None:
        body = {
            "source_id": req.source_id,
            "target_id": req.target_id,
            "relation_type": req.relation_type,
            "auto_create": req.auto_create,
        }
        if req.weight is not None:
            body["weight"] = req.weight
        self._c._post("/edge", body)

    def re_embed(self, req: ReEmbedRequest) -> ReEmbedResult:
        body: Dict[str, Any] = {"dim": req.dim}
        if req.batch_size is not None:
            body["batch_size"] = req.batch_size
        if req.model is not None:
            body["model"] = req.model
        data = self._c._post("/admin/re-embed", body)
        return ReEmbedResult(**data)


class TaskClient:
    """Task lifecycle operations."""

    def __init__(self, client: Client):
        self._c = client

    def create(self, req: TaskCreateRequest) -> TaskCreateResponse:
        body: Dict[str, Any] = {"content": req.content}
        if req.id is not None:
            body["id"] = req.id
        if req.context_ids is not None:
            body["context_ids"] = req.context_ids
        data = self._c._post("/task/create", body)
        return TaskCreateResponse(id=data["id"], status=data["status"])

    def status(self, req: TaskStatusRequest) -> None:
        self._c._post("/task/status", {"id": req.id, "status": req.status})

    def list_tasks(self, req: TaskListRequest) -> TaskExecutableResponse:
        body: Dict[str, Any] = {}
        if req.status:
            body["status"] = req.status
        if req.goal_id:
            body["goal_id"] = req.goal_id
        data = self._c._post("/task/list", body)
        return TaskExecutableResponse(
            tasks=[Entity(**t) for t in data.get("tasks", [])]
        )

    def show(self, req: TaskShowRequest) -> TaskShowResponse:
        data = self._c._post("/task/show", {"id": req.id})
        return TaskShowResponse(
            entity=Entity(**data["entity"]),
            blocked_by=[Edge(**e) for e in data.get("blocked_by", [])],
            recovers_via=[Edge(**e) for e in data.get("recovers_via", [])],
        )

    def dep(self, req: TaskDepRequest) -> None:
        self._c._post(
            "/task/dep",
            {
                "source_id": req.source_id,
                "target_id": req.target_id,
                "relation_type": req.relation_type,
                "add": req.add,
            },
        )

    def tree(self, req: TaskTreeRequest) -> TaskTreeResponse:
        data = self._c._post("/task/tree", {"goal_id": req.goal_id})
        return TaskTreeResponse(tree=data["tree"])

    def rollback(self, req: TaskRollbackRequest) -> TaskRollbackResponse:
        data = self._c._post("/task/rollback", {"id": req.id})
        return TaskRollbackResponse(rollback_task_id=data["rollback_task_id"])

    def executable(self, req: TaskListRequest) -> TaskExecutableResponse:
        body: Dict[str, Any] = {}
        if req.goal_id:
            body["goal_id"] = req.goal_id
        data = self._c._post("/task/executable", body)
        return TaskExecutableResponse(
            tasks=[Entity(**t) for t in data.get("tasks", [])]
        )

    def next(self, req: TaskListRequest) -> TaskExecutableResponse:
        return self.executable(req)


class GraphClient:
    """Graph analytics and timeline operations."""

    def __init__(self, client: Client):
        self._c = client

    def verify(self) -> VerifyReport:
        data = self._c._get("/graph/verify")
        return VerifyReport(issues=data.get("issues", []))

    def contradictions(self, entity_id: str = "") -> List[ContradictionPair]:
        path = "/contradictions"
        if entity_id:
            path += f"?id={entity_id}"
        data = self._c._get(path)
        return [ContradictionPair(**c) for c in data]

    def connected_components(self, min_size: int = 2) -> List[ConnectedComponent]:
        path = f"/connected-components?min_size={min_size}"
        data = self._c._get(path)
        return [ConnectedComponent(**c) for c in data]

    def communities(self, min_size: int = 0, max_iterations: int = 0) -> Dict[str, Any]:
        params: Dict[str, str] = {}
        if min_size > 0:
            params["min_size"] = str(min_size)
        if max_iterations > 0:
            params["max_iterations"] = str(max_iterations)
        path = "/communities"
        if params:
            path += "?" + urlencode(params)
        return self._c._get(path)

    def timeline(self, limit: int = 50) -> List[TimelineEntry]:
        path = f"/timeline?limit={limit}"
        data = self._c._get(path)
        return [TimelineEntry(**t) for t in data]

    def provenance(
        self,
        conversation_id: str = "",
        message_id: str = "",
        source: str = "",
        limit: int = 0,
    ) -> List[Entity]:
        params: Dict[str, str] = {}
        if conversation_id:
            params["conversation_id"] = conversation_id
        if message_id:
            params["message_id"] = message_id
        if source:
            params["source"] = source
        if limit > 0:
            params["limit"] = str(limit)
        path = "/provenance"
        if params:
            path += "?" + urlencode(params)
        data = self._c._get(path)
        return [Entity(**e) for e in data]

    def recovery_plan(self, task_id: str) -> List[Entity]:
        path = f"/recovery-plan?id={task_id}"
        data = self._c._get(path)
        return [Entity(**e) for e in data]


class AdminClient:
    """Administrative operations."""

    def __init__(self, client: Client):
        self._c = client

    def health(self) -> HealthResponse:
        data = self._c._get("/health")
        return HealthResponse(status=data["status"])

    def ready(self) -> ReadyResponse:
        data = self._c._get("/health/ready")
        return ReadyResponse(
            status=data["status"],
            latency_ms=data.get("latency_ms", 0),
            checks=data.get("checks"),
        )


def _parse_retrieval_result(data: Dict[str, Any]) -> RetrievalResult:
    return RetrievalResult(
        seed_nodes=[GraphNode(entity=Entity(**n["entity"]), **{k: v for k, v in n.items() if k != "entity"}) for n in data.get("seed_nodes", [])],
        world_facts=[RetrievedFact(**f) for f in data.get("world_facts", [])],
        opinions=[RetrievedFact(**f) for f in data.get("opinions", [])],
        experiences=[RetrievedFact(**f) for f in data.get("experiences", [])],
        observations=[RetrievedFact(**f) for f in data.get("observations", [])],
    )


def _parse_major(version: str) -> int:
    """Extract the MAJOR component from a semver string."""
    try:
        return int(version.split(".")[0])
    except (ValueError, IndexError):
        return 0
