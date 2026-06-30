/** Hermem TypeScript SDK — official client for the Hermem API. */

import type {
  APIErrorData,
  CheckResult,
  Community,
  ConnectedComponent,
  ContradictionPair,
  EdgeRequest,
  Entity,
  HealthResponse,
  IngestRequest,
  MigStatus,
  QueryResponse,
  ReadyResponse,
  ReEmbedRequest,
  ReEmbedResult,
  RetrieveRequest,
  RetrievalResult,
  SchemaReport,
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
} from "./types";

export const SDK_VERSION = "0.1.0";

export interface APIErrorListeners {
  versionMismatch: (serverVersion: string, sdkVersion: string) => void;
}

export class APIError extends Error {
  statusCode: number;
  code: string;
  field: string;

  constructor(statusCode: number, message: string, code = "", field = "") {
    super(`hermem: ${message} (status=${statusCode})`);
    this.name = "APIError";
    this.statusCode = statusCode;
    this.code = code;
    this.field = field;
  }
}

export interface ClientOptions {
  apiKey?: string;
  timeout?: number;
  /** Called (once) on first response when server MAJOR differs from SDK MAJOR. */
  onVersionMismatch?: (serverVersion: string, sdkVersion: string) => void;
}

export class Client {
  private baseUrl: string;
  private apiKey: string;
  private timeout: number;
  private onVersionMismatch?: (serverVersion: string, sdkVersion: string) => void;
  private versionChecked = false;

  readonly memory: MemoryClient;
  readonly task: TaskClient;
  readonly graph: GraphClient;
  readonly admin: AdminClient;

  constructor(baseUrl: string, options: ClientOptions = {}) {
    this.baseUrl = baseUrl.replace(/\/+$/, "");
    this.apiKey = options.apiKey ?? "";
    this.timeout = options.timeout ?? 30_000;
    this.onVersionMismatch = options.onVersionMismatch;
    this.memory = new MemoryClient(this);
    this.task = new TaskClient(this);
    this.graph = new GraphClient(this);
    this.admin = new AdminClient(this);
  }

  async do<T>(
    method: string,
    path: string,
    body?: unknown,
    _resultType?: new () => T,
  ): Promise<T> {
    const url = this.baseUrl + path;
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (this.apiKey) {
      headers["X-API-Key"] = this.apiKey;
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeout);

    try {
      const resp = await fetch(url, {
        method,
        headers,
        body: body != null ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });

      this.checkVersionMismatch(resp);

      if (!resp.ok) {
        const text = await resp.text();
        try {
          const data = JSON.parse(text) as APIErrorData;
          throw new APIError(resp.status, data.error, data.code ?? "", data.field ?? "");
        } catch (e) {
          if (e instanceof APIError) throw e;
          throw new APIError(resp.status, text);
        }
      }

      const text = await resp.text();
      if (text) {
        return JSON.parse(text) as T;
      }
      return undefined as T;
    } finally {
      clearTimeout(timer);
    }
  }

  private checkVersionMismatch(resp: Response): void {
    if (this.versionChecked) return;
    this.versionChecked = true;

    const serverVersion = resp.headers.get("X-Hermem-API-Version");
    if (!serverVersion) return;

    const serverMajor = parseMajor(serverVersion);
    const sdkMajor = parseMajor(SDK_VERSION);
    if (serverMajor !== sdkMajor && this.onVersionMismatch) {
      this.onVersionMismatch(serverVersion, SDK_VERSION);
    }
  }
}

function parseMajor(version: string): number {
  const major = parseInt(version.split(".")[0], 10);
  return isNaN(major) ? 0 : major;
}

class MemoryClient {
  constructor(private c: Client) {}

  async store(req: StoreRequest): Promise<void> {
    await this.c.do("POST", "/store", req);
  }

  async search(req: SearchRequest): Promise<SearchResult[]> {
    return this.c.do<SearchResult[]>("POST", "/search", {
      query: req.query,
      top_k: req.top_k ?? 5,
    });
  }

  async retrieve(req: RetrieveRequest): Promise<RetrievalResult> {
    return this.c.do<RetrievalResult>("POST", "/retrieve", {
      seed_ids: req.seed_ids,
      max_depth: req.max_depth ?? 2,
    });
  }

  async query(req: SearchRequest): Promise<QueryResponse> {
    return this.c.do<QueryResponse>("POST", "/query", {
      query: req.query,
      top_k: req.top_k ?? 5,
    });
  }

  async explain(req: SearchRequest): Promise<RetrievalResult> {
    return this.c.do<RetrievalResult>("POST", "/query/explain", {
      query: req.query,
      top_k: req.top_k ?? 5,
    });
  }

  async ingest(req: IngestRequest): Promise<void> {
    await this.c.do("POST", "/ingest", { dialog: req.dialog });
  }

  async edge(req: EdgeRequest): Promise<void> {
    await this.c.do("POST", "/edge", {
      source_id: req.source_id,
      target_id: req.target_id,
      relation_type: req.relation_type,
      auto_create: req.auto_create ?? false,
      weight: req.weight,
    });
  }

  async reEmbed(req: ReEmbedRequest): Promise<ReEmbedResult> {
    return this.c.do<ReEmbedResult>("POST", "/admin/re-embed", {
      dim: req.dim,
      batch_size: req.batch_size,
      model: req.model,
    });
  }
}

class TaskClient {
  constructor(private c: Client) {}

  async create(req: TaskCreateRequest): Promise<TaskCreateResponse> {
    const body: Record<string, unknown> = { content: req.content };
    if (req.id) body.id = req.id;
    if (req.context_ids) body.context_ids = req.context_ids;
    return this.c.do<TaskCreateResponse>("POST", "/task/create", body);
  }

  async status(req: TaskStatusRequest): Promise<void> {
    await this.c.do("POST", "/task/status", {
      id: req.id,
      status: req.status,
    });
  }

  async list(req: TaskListRequest): Promise<TaskExecutableResponse> {
    const body: Record<string, string> = {};
    if (req.status) body.status = req.status;
    if (req.goal_id) body.goal_id = req.goal_id;
    return this.c.do<TaskExecutableResponse>("POST", "/task/list", body);
  }

  async show(req: TaskShowRequest): Promise<TaskShowResponse> {
    return this.c.do<TaskShowResponse>("POST", "/task/show", { id: req.id });
  }

  async dep(req: TaskDepRequest): Promise<void> {
    await this.c.do("POST", "/task/dep", {
      source_id: req.source_id,
      target_id: req.target_id,
      relation_type: req.relation_type ?? "blocked_by",
      add: req.add ?? true,
    });
  }

  async tree(req: TaskTreeRequest): Promise<TaskTreeResponse> {
    return this.c.do<TaskTreeResponse>("POST", "/task/tree", {
      goal_id: req.goal_id,
    });
  }

  async rollback(req: TaskRollbackRequest): Promise<TaskRollbackResponse> {
    return this.c.do<TaskRollbackResponse>("POST", "/task/rollback", {
      id: req.id,
    });
  }

  async executable(
    req: TaskListRequest = {},
  ): Promise<TaskExecutableResponse> {
    const body: Record<string, string> = {};
    if (req.goal_id) body.goal_id = req.goal_id;
    return this.c.do<TaskExecutableResponse>("POST", "/task/executable", body);
  }

  async next(req: TaskListRequest = {}): Promise<TaskExecutableResponse> {
    return this.executable(req);
  }
}

class GraphClient {
  constructor(private c: Client) {}

  async verify(): Promise<VerifyReport> {
    return this.c.do<VerifyReport>("GET", "/graph/verify");
  }

  async contradictions(entityId = ""): Promise<ContradictionPair[]> {
    const path = entityId
      ? `/contradictions?id=${encodeURIComponent(entityId)}`
      : "/contradictions";
    return this.c.do<ContradictionPair[]>("GET", path);
  }

  async connectedComponents(minSize = 2): Promise<ConnectedComponent[]> {
    return this.c.do<ConnectedComponent[]>(
      "GET",
      `/connected-components?min_size=${minSize}`,
    );
  }

  async communities(
    params: { min_size?: number; max_iterations?: number } = {},
  ): Promise<{
    communities: Community[];
    global_modularity: number;
    total_communities: number;
  }> {
    const qs = new URLSearchParams();
    if (params.min_size) qs.set("min_size", String(params.min_size));
    if (params.max_iterations)
      qs.set("max_iterations", String(params.max_iterations));
    const q = qs.toString();
    return this.c.do("GET", `/communities${q ? "?" + q : ""}`);
  }

  async timeline(limit = 50): Promise<TimelineEntry[]> {
    return this.c.do<TimelineEntry[]>("GET", `/timeline?limit=${limit}`);
  }

  async provenance(params: {
    conversation_id?: string;
    message_id?: string;
    source?: string;
    limit?: number;
  } = {}): Promise<Entity[]> {
    const qs = new URLSearchParams();
    if (params.conversation_id)
      qs.set("conversation_id", params.conversation_id);
    if (params.message_id) qs.set("message_id", params.message_id);
    if (params.source) qs.set("source", params.source);
    if (params.limit) qs.set("limit", String(params.limit));
    const q = qs.toString();
    return this.c.do<Entity[]>("GET", `/provenance${q ? "?" + q : ""}`);
  }

  async recoveryPlan(taskId: string): Promise<Entity[]> {
    return this.c.do<Entity[]>(
      "GET",
      `/recovery-plan?id=${encodeURIComponent(taskId)}`,
    );
  }
}

class AdminClient {
  constructor(private c: Client) {}

  async health(): Promise<HealthResponse> {
    return this.c.do<HealthResponse>("GET", "/health");
  }

  async ready(): Promise<ReadyResponse> {
    return this.c.do<ReadyResponse>("GET", "/health/ready");
  }

  async migrateStatus(): Promise<MigStatus[]> {
    return this.c.do<MigStatus[]>("GET", "/db/migrate");
  }

  async schema(): Promise<SchemaReport> {
    return this.c.do<SchemaReport>("GET", "/db/schema");
  }

  async verifyDB(): Promise<void> {
    await this.c.do("GET", "/db/verify");
  }

  async rollback(): Promise<void> {
    await this.c.do("POST", "/db/rollback");
  }
}

export { MemoryClient, TaskClient, GraphClient, AdminClient };
