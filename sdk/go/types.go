package hermem

import "fmt"

// ErrorResponse is the standard API error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	Field string `json:"field,omitempty"`
}

// APIError is a structured error returned by the Hermem API.
type APIError struct {
	StatusCode int
	Message    string
	Code       string
	Field      string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("hermem: %s (code=%s, status=%d)", e.Message, e.Code, e.StatusCode)
	}
	return fmt.Sprintf("hermem: %s (status=%d)", e.Message, e.StatusCode)
}

// Entity is a stored fact, opinion, experience, or observation.
type Entity struct {
	ID             string    `json:"id"`
	Category       string    `json:"category"`
	Content        string    `json:"content"`
	Embedding      []float32 `json:"embedding,omitempty"`
	UpdatedAt      *string   `json:"updated_at,omitempty"`
	LastAccessedAt *string   `json:"last_accessed_at,omitempty"`
	Archived       bool      `json:"archived"`
	Status         string    `json:"status,omitempty"`
	Confidence     float32   `json:"confidence,omitempty"`
	Source         string    `json:"source,omitempty"`
	SourceType     string    `json:"source_type,omitempty"`
	CreatedAt      *string   `json:"created_at,omitempty"`
	ValidFrom      *string   `json:"valid_from,omitempty"`
	ValidTo        *string   `json:"valid_to,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	MessageID      string    `json:"message_id,omitempty"`
	ExtractedFrom  string    `json:"extracted_from,omitempty"`
	Degree         int       `json:"degree,omitempty"`
	Priority       int       `json:"priority,omitempty"`
}

// Edge is a directed relation between two entities.
type Edge struct {
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	Weight       float32 `json:"weight,omitempty"`
}

// --- Request types ---

type StoreRequest struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type RetrieveRequest struct {
	SeedIDs  []string `json:"seed_ids"`
	MaxDepth int      `json:"max_depth"`
}

type IngestRequest struct {
	Dialog string `json:"dialog"`
}

type EdgeRequest struct {
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	AutoCreate   bool    `json:"auto_create"`
	Weight       float32 `json:"weight,omitempty"`
}

type TaskStatusRequest struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type TaskListRequest struct {
	Status string `json:"status"`
	GoalID string `json:"goal_id"`
}

type TaskShowRequest struct {
	ID string `json:"id"`
}

type TaskDepRequest struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
	Add          bool   `json:"add"`
}

type TaskCreateRequest struct {
	ID         string   `json:"id,omitempty"`
	Content    string   `json:"content"`
	ContextIDs []string `json:"context_ids,omitempty"`
}

type TaskRollbackRequest struct {
	ID string `json:"id"`
}

type TaskTreeRequest struct {
	GoalID string `json:"goal_id"`
}

type ReEmbedRequest struct {
	Dim       int    `json:"dim"`
	BatchSize int    `json:"batch_size,omitempty"`
	Model     string `json:"model,omitempty"`
}

// --- Response types ---

type SearchResult struct {
	Entity     Entity  `json:"entity"`
	Similarity float32 `json:"similarity"`
}

type ScoreBreakdown struct {
	VectorScore     float32 `json:"vector_score"`
	RecencyScore    float32 `json:"recency_score"`
	TemporalScore   float32 `json:"temporal_score"`
	CentralityScore float32 `json:"centrality_score"`
	PathScore       float32 `json:"path_score"`
	DepthPenalty    float32 `json:"depth_penalty"`
	FinalScore      float32 `json:"final_score"`
}

type RetrievedFact struct {
	Content        string          `json:"content"`
	ParentID       string          `json:"parent_id,omitempty"`
	RelationType   string          `json:"relation_type,omitempty"`
	Depth          int             `json:"depth"`
	RankingScore   float32         `json:"ranking_score,omitempty"`
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`
}

type GraphNode struct {
	Entity         Entity          `json:"entity"`
	Relations      []Edge          `json:"relations,omitempty"`
	Depth          int             `json:"depth"`
	PathWeight     float32         `json:"path_weight,omitempty"`
	ParentID       string          `json:"parent_id"`
	RelationType   string          `json:"relation_type,omitempty"`
	RankingScore   float32         `json:"ranking_score"`
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`
}

type RetrievalResult struct {
	SeedNodes    []GraphNode     `json:"seed_nodes"`
	WorldFacts   []RetrievedFact `json:"world_facts"`
	Opinions     []RetrievedFact `json:"opinions"`
	Experiences  []RetrievedFact `json:"experiences"`
	Observations []RetrievedFact `json:"observations"`
}

type TaskShowResponse struct {
	Entity      Entity `json:"entity"`
	BlockedBy   []Edge `json:"blocked_by"`
	RecoversVia []Edge `json:"recovers_via"`
}

type TaskRollbackResponse struct {
	RollbackTaskID string `json:"rollback_task_id"`
}

type TaskTreeResponse struct {
	Tree string `json:"tree"`
}

type TaskCreateResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type TaskExecutableResponse struct {
	Tasks []Entity `json:"tasks"`
}

type ContradictionPair struct {
	SourceID      string `json:"source_id"`
	SourceContent string `json:"source_content"`
	TargetID      string `json:"target_id"`
	TargetContent string `json:"target_content"`
}

type ConnectedComponent struct {
	IDs       []string `json:"ids"`
	Size      int      `json:"size"`
	AvgDegree float64  `json:"avg_degree"`
}

type Community struct {
	ID         string   `json:"id"`
	Members    []string `json:"members"`
	Size       int      `json:"size"`
	Modularity float64  `json:"modularity"`
}

type VerifyReport struct {
	Issues []string `json:"issues"`
}

type ReEmbedResult struct {
	TotalEntities int    `json:"total_entities"`
	ReEmbedded    int    `json:"re_embedded"`
	Skipped       int    `json:"skipped"`
	Failed        int    `json:"failed"`
	Elapsed       string `json:"elapsed"`
	OldDim        int    `json:"old_dim"`
	NewDim        int    `json:"new_dim"`
	Batches       int    `json:"batches"`
}

type TimelineEntry struct {
	ID             string `json:"id"`
	Category       string `json:"category"`
	Content        string `json:"content"`
	CreatedAt      string `json:"created_at"`
	Source         string `json:"source,omitempty"`
	SourceType     string `json:"source_type,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
}

type QueryResponse struct {
	Context string `json:"context"`
}

type ResponseResult struct {
	Response string `json:"response"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type ReadyResponse struct {
	Status    string                 `json:"status"`
	LatencyMs int                    `json:"latency_ms"`
	Checks    map[string]CheckResult `json:"checks,omitempty"`
}

type CheckResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	Critical  bool   `json:"critical"`
}

type MigStatus struct {
	Name           string  `json:"name"`
	Applied        bool    `json:"applied"`
	AppliedAt      *string `json:"applied_at,omitempty"`
	ChecksumSHA256 string  `json:"checksum_sha256,omitempty"`
	ChecksumMatch  bool    `json:"checksum_match,omitempty"`
}

type SchemaReport struct {
	Stored        string `json:"stored"`
	Current       string `json:"current"`
	DriftDetected bool   `json:"drift_detected"`
}
