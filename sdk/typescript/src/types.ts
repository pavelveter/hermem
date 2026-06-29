/** Type definitions for the Hermem TypeScript SDK. */

export interface Entity {
  id: string;
  category: string;
  content: string;
  embedding?: number[];
  updated_at?: string;
  last_accessed_at?: string;
  archived: boolean;
  status?: string;
  confidence?: number;
  source?: string;
  source_type?: string;
  created_at?: string;
  valid_from?: string;
  valid_to?: string;
  conversation_id?: string;
  message_id?: string;
  extracted_from?: string;
  degree?: number;
  priority?: number;
}

export interface Edge {
  source_id: string;
  target_id: string;
  relation_type: string;
  weight?: number;
}

export interface StoreRequest {
  id: string;
  category: string;
  content: string;
  embedding?: number[];
}

export interface SearchRequest {
  query: string;
  top_k?: number;
}

export interface RetrieveRequest {
  seed_ids: string[];
  max_depth?: number;
}

export interface IngestRequest {
  dialog: string;
}

export interface EdgeRequest {
  source_id: string;
  target_id: string;
  relation_type: string;
  auto_create?: boolean;
  weight?: number;
}

export interface SearchResult {
  entity: Entity;
  similarity: number;
}

export interface ScoreBreakdown {
  vector_score: number;
  recency_score: number;
  temporal_score: number;
  centrality_score: number;
  path_score: number;
  depth_penalty: number;
  final_score: number;
}

export interface RetrievedFact {
  content: string;
  parent_id?: string;
  relation_type?: string;
  depth: number;
  ranking_score?: number;
  score_breakdown?: ScoreBreakdown;
}

export interface GraphNode {
  entity: Entity;
  relations?: Edge[];
  depth: number;
  path_weight?: number;
  parent_id: string;
  relation_type?: string;
  ranking_score: number;
  score_breakdown?: ScoreBreakdown;
}

export interface RetrievalResult {
  seed_nodes: GraphNode[];
  world_facts: RetrievedFact[];
  opinions: RetrievedFact[];
  experiences: RetrievedFact[];
  observations: RetrievedFact[];
}

export interface TaskStatusRequest {
  id: string;
  status: string;
}

export interface TaskListRequest {
  status?: string;
  goal_id?: string;
}

export interface TaskShowRequest {
  id: string;
}

export interface TaskShowResponse {
  entity: Entity;
  blocked_by: Edge[];
  recovers_via: Edge[];
}

export interface TaskDepRequest {
  source_id: string;
  target_id: string;
  relation_type?: string;
  add?: boolean;
}

export interface TaskCreateRequest {
  content: string;
  id?: string;
  context_ids?: string[];
}

export interface TaskCreateResponse {
  id: string;
  status: string;
}

export interface TaskRollbackRequest {
  id: string;
}

export interface TaskRollbackResponse {
  rollback_task_id: string;
}

export interface TaskTreeRequest {
  goal_id?: string;
}

export interface TaskTreeResponse {
  tree: string;
}

export interface TaskExecutableResponse {
  tasks: Entity[];
}

export interface ContradictionPair {
  source_id: string;
  source_content: string;
  target_id: string;
  target_content: string;
}

export interface ConnectedComponent {
  ids: string[];
  size: number;
  avg_degree: number;
}

export interface Community {
  id: string;
  members: string[];
  size: number;
  modularity: number;
}

export interface VerifyReport {
  issues: string[];
}

export interface ReEmbedRequest {
  dim: number;
  batch_size?: number;
  model?: string;
}

export interface ReEmbedResult {
  total_entities: number;
  re_embedded: number;
  skipped: number;
  failed: number;
  elapsed: string;
  old_dim: number;
  new_dim: number;
  batches: number;
}

export interface TimelineEntry {
  id: string;
  category: string;
  content: string;
  created_at: string;
  source?: string;
  source_type?: string;
  conversation_id?: string;
  message_id?: string;
}

export interface QueryResponse {
  context: string;
}

export interface HealthResponse {
  status: string;
}

export interface ReadyResponse {
  status: string;
  latency_ms: number;
  checks?: Record<string, CheckResult>;
}

export interface CheckResult {
  ok: boolean;
  latency_ms: number;
  error?: string;
  critical: boolean;
}

export interface MigStatus {
  name: string;
  applied: boolean;
  applied_at?: string;
  checksum_sha256?: string;
  checksum_match?: boolean;
}

export interface SchemaReport {
  stored: string;
  current: string;
  drift_detected: boolean;
}

export interface APIErrorData {
  error: string;
  code?: string;
  field?: string;
}
