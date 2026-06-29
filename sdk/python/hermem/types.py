"""Type definitions for the Hermem Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


class APIError(Exception):
    """Structured error returned by the Hermem API."""

    def __init__(self, status_code: int, message: str, code: str = "", field: str = ""):
        self.status_code = status_code
        self.message = message
        self.code = code
        self.field = field
        super().__init__(f"hermem: {message} (status={status_code})")


@dataclass
class Entity:
    id: str
    category: str
    content: str
    embedding: Optional[List[float]] = None
    updated_at: Optional[str] = None
    last_accessed_at: Optional[str] = None
    archived: bool = False
    status: Optional[str] = None
    confidence: Optional[float] = None
    source: Optional[str] = None
    source_type: Optional[str] = None
    created_at: Optional[str] = None
    valid_from: Optional[str] = None
    valid_to: Optional[str] = None
    conversation_id: Optional[str] = None
    message_id: Optional[str] = None
    extracted_from: Optional[str] = None
    degree: Optional[int] = None
    priority: Optional[int] = None


@dataclass
class Edge:
    source_id: str
    target_id: str
    relation_type: str
    weight: Optional[float] = None


@dataclass
class StoreRequest:
    id: str
    category: str
    content: str
    embedding: Optional[List[float]] = None


@dataclass
class SearchRequest:
    query: str
    top_k: int = 5


@dataclass
class RetrieveRequest:
    seed_ids: List[str]
    max_depth: int = 2


@dataclass
class IngestRequest:
    dialog: str


@dataclass
class EdgeRequest:
    source_id: str
    target_id: str
    relation_type: str
    auto_create: bool = False
    weight: Optional[float] = None


@dataclass
class SearchResult:
    entity: Entity
    similarity: float


@dataclass
class ScoreBreakdown:
    vector_score: float = 0.0
    recency_score: float = 0.0
    temporal_score: float = 0.0
    centrality_score: float = 0.0
    path_score: float = 0.0
    depth_penalty: float = 0.0
    final_score: float = 0.0


@dataclass
class RetrievedFact:
    content: str
    parent_id: Optional[str] = None
    relation_type: Optional[str] = None
    depth: int = 0
    ranking_score: Optional[float] = None
    score_breakdown: Optional[ScoreBreakdown] = None


@dataclass
class GraphNode:
    entity: Entity
    depth: int = 0
    path_weight: Optional[float] = None
    parent_id: Optional[str] = None
    relation_type: Optional[str] = None
    ranking_score: float = 0.0
    score_breakdown: Optional[ScoreBreakdown] = None
    relations: Optional[List[Edge]] = None


@dataclass
class RetrievalResult:
    seed_nodes: List[GraphNode] = field(default_factory=list)
    world_facts: List[RetrievedFact] = field(default_factory=list)
    opinions: List[RetrievedFact] = field(default_factory=list)
    experiences: List[RetrievedFact] = field(default_factory=list)
    observations: List[RetrievedFact] = field(default_factory=list)


@dataclass
class TaskStatusRequest:
    id: str
    status: str


@dataclass
class TaskListRequest:
    status: Optional[str] = None
    goal_id: Optional[str] = None


@dataclass
class TaskShowRequest:
    id: str


@dataclass
class TaskShowResponse:
    entity: Entity
    blocked_by: List[Edge] = field(default_factory=list)
    recovers_via: List[Edge] = field(default_factory=list)


@dataclass
class TaskDepRequest:
    source_id: str
    target_id: str
    relation_type: str = "blocked_by"
    add: bool = True


@dataclass
class TaskCreateRequest:
    content: str
    id: Optional[str] = None
    context_ids: Optional[List[str]] = None


@dataclass
class TaskCreateResponse:
    id: str
    status: str


@dataclass
class TaskRollbackRequest:
    id: str


@dataclass
class TaskRollbackResponse:
    rollback_task_id: str


@dataclass
class TaskTreeResponse:
    tree: str


@dataclass
class TaskExecutableResponse:
    tasks: List[Entity] = field(default_factory=list)


@dataclass
class ContradictionPair:
    source_id: str
    source_content: str
    target_id: str
    target_content: str


@dataclass
class ConnectedComponent:
    ids: List[str] = field(default_factory=list)
    size: int = 0
    avg_degree: float = 0.0


@dataclass
class Community:
    id: str
    members: List[str] = field(default_factory=list)
    size: int = 0
    modularity: float = 0.0


@dataclass
class VerifyReport:
    issues: List[str] = field(default_factory=list)


@dataclass
class ReEmbedRequest:
    dim: int
    batch_size: Optional[int] = None
    model: Optional[str] = None


@dataclass
class ReEmbedResult:
    total_entities: int = 0
    re_embedded: int = 0
    skipped: int = 0
    failed: int = 0
    elapsed: str = ""
    old_dim: int = 0
    new_dim: int = 0
    batches: int = 0


@dataclass
class TimelineEntry:
    id: str
    category: str
    content: str
    created_at: str
    source: Optional[str] = None
    source_type: Optional[str] = None
    conversation_id: Optional[str] = None
    message_id: Optional[str] = None


@dataclass
class QueryResponse:
    context: str


@dataclass
class HealthResponse:
    status: str


@dataclass
class ReadyResponse:
    status: str
    latency_ms: int = 0
    checks: Optional[Dict[str, Any]] = None
