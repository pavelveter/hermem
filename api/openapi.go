// Package api provides the OpenAPI 3.1 specification for the Hermem HTTP API.
// The spec is defined as Go structs that marshal to JSON/YAML, ensuring
// the documentation stays synchronized with the implementation.
package api

import (
	"encoding/json"
	"time"

	"gopkg.in/yaml.v3"
)

// Spec is the root OpenAPI 3.1 document.
type Spec struct {
	OpenAPI    string               `json:"openapi" yaml:"openapi"`
	Info       Info                 `json:"info" yaml:"info"`
	Servers    []Server             `json:"servers,omitempty" yaml:"servers,omitempty"`
	Paths      map[string]*PathItem `json:"paths" yaml:"paths"`
	Components Components           `json:"components" yaml:"components"`
	Tags       []Tag                `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// Info describes the API.
type Info struct {
	Title       string   `json:"title" yaml:"title"`
	Description string   `json:"description" yaml:"description"`
	Version     string   `json:"version" yaml:"version"`
	License     *License `json:"license,omitempty" yaml:"license,omitempty"`
}

// License identifies the license.
type License struct {
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
}

// Server is a connectivity target.
type Server struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Tag groups operations.
type Tag struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PathItem holds operations for one path.
type PathItem struct {
	Get    *Operation `json:"get,omitempty" yaml:"get,omitempty"`
	Post   *Operation `json:"post,omitempty" yaml:"post,omitempty"`
	Delete *Operation `json:"delete,omitempty" yaml:"delete,omitempty"`
	Put    *Operation `json:"put,omitempty" yaml:"put,omitempty"`
}

// Operation is one API operation.
type Operation struct {
	Summary     string                `json:"summary" yaml:"summary"`
	Description string                `json:"description,omitempty" yaml:"description,omitempty"`
	OperationID string                `json:"operationId" yaml:"operationId"`
	Tags        []string              `json:"tags,omitempty" yaml:"tags,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses" yaml:"responses"`
	Security    []SecurityRequirement `json:"security,omitempty" yaml:"security,omitempty"`
}

// Parameter is a single parameter.
type Parameter struct {
	Name     string  `json:"name" yaml:"name"`
	In       string  `json:"in" yaml:"in"`
	Required bool    `json:"required,omitempty" yaml:"required,omitempty"`
	Schema   *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// RequestBody describes the request body.
type RequestBody struct {
	Required bool                 `json:"required,omitempty" yaml:"required,omitempty"`
	Content  map[string]MediaType `json:"content" yaml:"content"`
}

// Response is a single response.
type Response struct {
	Description string               `json:"description" yaml:"description"`
	Content     map[string]MediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

// MediaType describes a single media type.
type MediaType struct {
	Schema   *Schema            `json:"schema,omitempty" yaml:"schema,omitempty"`
	Example  interface{}        `json:"example,omitempty" yaml:"example,omitempty"`
	Examples map[string]Example `json:"examples,omitempty" yaml:"examples,omitempty"`
}

// Example is a single example.
type Example struct {
	Summary     string      `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string      `json:"description,omitempty" yaml:"description,omitempty"`
	Value       interface{} `json:"value,omitempty" yaml:"value,omitempty"`
}

// Schema is a JSON Schema.
type Schema struct {
	Type                 string             `json:"type,omitempty" yaml:"type,omitempty"`
	Description          string             `json:"description,omitempty" yaml:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty" yaml:"properties,omitempty"`
	Required             []string           `json:"required,omitempty" yaml:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty" yaml:"items,omitempty"`
	Ref                  string             `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Enum                 []string           `json:"enum,omitempty" yaml:"enum,omitempty"`
	Default              interface{}        `json:"default,omitempty" yaml:"default,omitempty"`
	Format               string             `json:"format,omitempty" yaml:"format,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	Deprecated           bool               `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
}

// Components holds reusable definitions.
type Components struct {
	Schemas         map[string]*Schema        `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
}

// SecurityScheme describes an authentication scheme.
type SecurityScheme struct {
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	In          string `json:"in,omitempty" yaml:"in,omitempty"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Scheme      string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
}

// SecurityRequirement references security schemes.
type SecurityRequirement = map[string][]string

// GenerateSpec builds the complete OpenAPI 3.1 specification.
func GenerateSpec() *Spec {
	s := &Spec{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:       "Hermem API",
			Description: "Persistent graph memory for LLM agents. SQLite. Embeddings. Graph traversal. One binary.",
			Version:     time.Now().Format("2006.01.02"),
			License:     &License{Name: "MIT"},
		},
		Servers: []Server{
			{URL: "http://localhost:8420", Description: "Local development"},
		},
		Tags: []Tag{
			{Name: "memory", Description: "Entity storage, search, and retrieval"},
			{Name: "ingest", Description: "Dialog ingestion and entity extraction"},
			{Name: "task", Description: "Task lifecycle management"},
			{Name: "graph", Description: "Graph analytics and integrity"},
			{Name: "temporal", Description: "Time-based queries and timeline"},
			{Name: "admin", Description: "Administrative operations"},
			{Name: "health", Description: "Health checks and metrics"},
		},
		Components: Components{
			SecuritySchemes: map[string]SecurityScheme{
				"ApiKeyAuth": {
					Type: "apiKey",
					In:   "header",
					Name: "X-API-Key",
				},
			},
			Schemas: buildSchemas(),
		},
	}

	s.Paths = buildPaths()
	return s
}

func buildSchemas() map[string]*Schema {
	return map[string]*Schema{
		"Entity": entitySchema(),
		"Edge":   edgeSchema(),
		"ErrorResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"error": {Type: "string"},
				"code":  {Type: "string"},
				"field": {Type: "string"},
			},
			Required: []string{"error"},
		},
		"StoreRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"id":        {Type: "string", Description: "Unique entity identifier"},
				"category":  {Type: "string", Enum: []string{"world", "opinion", "experience", "observation", "task"}},
				"content":   {Type: "string", Description: "Entity content text"},
				"embedding": {Type: "array", Items: &Schema{Type: "number", Format: "float"}},
			},
			Required: []string{"id", "category", "content"},
		},
		"SearchRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"query": {Type: "string", Description: "Search query text"},
				"top_k": {Type: "integer", Default: 5, Description: "Number of results"},
			},
			Required: []string{"query"},
		},
		"RetrieveRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"seed_ids":  {Type: "array", Items: &Schema{Type: "string"}},
				"max_depth": {Type: "integer", Default: 2},
			},
			Required: []string{"seed_ids"},
		},
		"IngestRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"dialog": {Type: "string", Description: "Conversational text to ingest"},
			},
			Required: []string{"dialog"},
		},
		"EdgeRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"source_id":     {Type: "string"},
				"target_id":     {Type: "string"},
				"relation_type": {Type: "string", Enum: []string{"prefers", "uses", "mentions", "related_to", "part_of", "causes", "contradicts", "blocked_by", "recovers_via"}},
				"auto_create":   {Type: "boolean", Default: false},
				"weight":        {Type: "number", Format: "float", Default: 1.0},
			},
			Required: []string{"source_id", "target_id", "relation_type"},
		},
		"SearchResult": {
			Type: "object",
			Properties: map[string]*Schema{
				"entity":     {Ref: "#/components/schemas/Entity"},
				"similarity": {Type: "number", Format: "float"},
			},
		},
		"RetrievalResult": retrievalResultSchema(),
		"RetrievedFact": {
			Type: "object",
			Properties: map[string]*Schema{
				"content":         {Type: "string"},
				"parent_id":       {Type: "string"},
				"relation_type":   {Type: "string"},
				"depth":           {Type: "integer"},
				"ranking_score":   {Type: "number", Format: "float"},
				"score_breakdown": {Ref: "#/components/schemas/ScoreBreakdown"},
			},
		},
		"GraphNode": {
			Type: "object",
			Properties: map[string]*Schema{
				"entity":          {Ref: "#/components/schemas/Entity"},
				"relations":       {Type: "array", Items: &Schema{Ref: "#/components/schemas/Edge"}},
				"depth":           {Type: "integer"},
				"path_weight":     {Type: "number", Format: "float"},
				"parent_id":       {Type: "string"},
				"relation_type":   {Type: "string"},
				"ranking_score":   {Type: "number", Format: "float"},
				"score_breakdown": {Ref: "#/components/schemas/ScoreBreakdown"},
			},
		},
		"ScoreBreakdown": {
			Type: "object",
			Properties: map[string]*Schema{
				"vector_score":     {Type: "number", Format: "float"},
				"recency_score":    {Type: "number", Format: "float"},
				"temporal_score":   {Type: "number", Format: "float"},
				"centrality_score": {Type: "number", Format: "float"},
				"path_score":       {Type: "number", Format: "float"},
				"depth_penalty":    {Type: "number", Format: "float"},
				"final_score":      {Type: "number", Format: "float"},
			},
		},
		"TaskStatusRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"id":     {Type: "string"},
				"status": {Type: "string", Enum: []string{"pending", "running", "completed", "failed"}},
			},
			Required: []string{"id", "status"},
		},
		"TaskListRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"status":  {Type: "string"},
				"goal_id": {Type: "string"},
			},
		},
		"TaskShowRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"id": {Type: "string"},
			},
			Required: []string{"id"},
		},
		"TaskShowResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"entity":       {Ref: "#/components/schemas/Entity"},
				"blocked_by":   {Type: "array", Items: &Schema{Ref: "#/components/schemas/Edge"}},
				"recovers_via": {Type: "array", Items: &Schema{Ref: "#/components/schemas/Edge"}},
			},
		},
		"TaskDepRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"source_id":     {Type: "string"},
				"target_id":     {Type: "string"},
				"relation_type": {Type: "string", Default: "blocked_by"},
				"add":           {Type: "boolean", Default: true},
			},
			Required: []string{"source_id", "target_id"},
		},
		"TaskCreateRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"id":          {Type: "string", Description: "Auto-generated if omitted"},
				"content":     {Type: "string"},
				"context_ids": {Type: "array", Items: &Schema{Type: "string"}},
			},
			Required: []string{"content"},
		},
		"TaskCreateResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"id":     {Type: "string"},
				"status": {Type: "string"},
			},
		},
		"TaskRollbackRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"id": {Type: "string"},
			},
			Required: []string{"id"},
		},
		"TaskRollbackResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"rollback_task_id": {Type: "string"},
			},
		},
		"TaskTreeRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"goal_id": {Type: "string"},
			},
		},
		"TaskTreeResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"tree": {Type: "string", Description: "ASCII tree rendering"},
			},
		},
		"TaskExecutableResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"tasks": {Type: "array", Items: &Schema{Ref: "#/components/schemas/Entity"}},
			},
		},
		"ContradictionPair": {
			Type: "object",
			Properties: map[string]*Schema{
				"source_id":      {Type: "string"},
				"source_content": {Type: "string"},
				"target_id":      {Type: "string"},
				"target_content": {Type: "string"},
			},
		},
		"ConnectedComponent": {
			Type: "object",
			Properties: map[string]*Schema{
				"ids":        {Type: "array", Items: &Schema{Type: "string"}},
				"size":       {Type: "integer"},
				"avg_degree": {Type: "number", Format: "double"},
			},
		},
		"Community": {
			Type: "object",
			Properties: map[string]*Schema{
				"id":         {Type: "string"},
				"members":    {Type: "array", Items: &Schema{Type: "string"}},
				"size":       {Type: "integer"},
				"modularity": {Type: "number", Format: "double"},
			},
		},
		"VerifyReport": {
			Type: "object",
			Properties: map[string]*Schema{
				"issues": {Type: "array", Items: &Schema{Type: "string"}},
			},
		},
		"ReEmbedRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"dim":        {Type: "integer"},
				"batch_size": {Type: "integer", Default: 50},
				"model":      {Type: "string"},
			},
			Required: []string{"dim"},
		},
		"ReEmbedResult": {
			Type: "object",
			Properties: map[string]*Schema{
				"total_entities": {Type: "integer"},
				"re_embedded":    {Type: "integer"},
				"skipped":        {Type: "integer"},
				"failed":         {Type: "integer"},
				"elapsed":        {Type: "string"},
				"old_dim":        {Type: "integer"},
				"new_dim":        {Type: "integer"},
				"batches":        {Type: "integer"},
			},
		},
		"HealthResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"status": {Type: "string"},
			},
		},
		"ReadyResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"status":     {Type: "string"},
				"latency_ms": {Type: "integer"},
				"checks": {
					Type: "object",
					AdditionalProperties: &Schema{
						Type: "object",
						Properties: map[string]*Schema{
							"ok":         {Type: "boolean"},
							"latency_ms": {Type: "integer"},
							"critical":   {Type: "boolean"},
							"error":      {Type: "string"},
						},
					},
				},
			},
		},
		"TimelineEntry": {
			Type: "object",
			Properties: map[string]*Schema{
				"id":              {Type: "string"},
				"category":        {Type: "string"},
				"content":         {Type: "string"},
				"created_at":      {Type: "string", Format: "date-time"},
				"source":          {Type: "string"},
				"source_type":     {Type: "string"},
				"conversation_id": {Type: "string"},
				"message_id":      {Type: "string"},
			},
		},
		"MigStatus": {
			Type: "object",
			Properties: map[string]*Schema{
				"name":            {Type: "string"},
				"applied":         {Type: "boolean"},
				"applied_at":      {Type: "string", Format: "date-time"},
				"checksum_sha256": {Type: "string"},
				"checksum_match":  {Type: "boolean"},
			},
		},
		"SchemaReport": {
			Type: "object",
			Properties: map[string]*Schema{
				"stored":         {Type: "string"},
				"current":        {Type: "string"},
				"drift_detected": {Type: "boolean"},
			},
		},
		"QueryResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"context": {Type: "string", Description: "Markdown-formatted context"},
			},
		},
		"ResponseResult": {
			Type: "object",
			Properties: map[string]*Schema{
				"response": {Type: "string"},
			},
		},
		"Stats": {
			Type: "object",
			Properties: map[string]*Schema{
				"node_count":          {Type: "integer"},
				"edge_count":          {Type: "integer"},
				"archived_count":      {Type: "integer"},
				"contradiction_count": {Type: "integer"},
				"embedding_coverage":  {Type: "number", Format: "float"},
				"last_gc_run_at":      {Type: "string", Format: "date-time"},
				"last_gc_archived":    {Type: "integer"},
				"db_size_bytes":       {Type: "integer"},
				"captured_at":         {Type: "string", Format: "date-time"},
			},
		},
		"IntegrityReport": {
			Type: "object",
			Properties: map[string]*Schema{
				"ok":         {Type: "boolean"},
				"issues":     {Type: "array", Items: &Schema{Ref: "#/components/schemas/IntegrityIssue"}},
				"checked_at": {Type: "string", Format: "date-time"},
			},
		},
		"IntegrityIssue": {
			Type: "object",
			Properties: map[string]*Schema{
				"code":    {Type: "string"},
				"level":   {Type: "string", Enum: []string{"critical", "warning", "info"}},
				"subject": {Type: "string"},
				"message": {Type: "string"},
			},
		},
	}
}

func entitySchema() *Schema {
	return &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"id":               {Type: "string"},
			"category":         {Type: "string"},
			"content":          {Type: "string"},
			"embedding":        {Type: "array", Items: &Schema{Type: "number", Format: "float"}},
			"updated_at":       {Type: "string", Format: "date-time"},
			"last_accessed_at": {Type: "string", Format: "date-time"},
			"archived":         {Type: "boolean"},
			"status":           {Type: "string"},
			"confidence":       {Type: "number", Format: "float"},
			"source":           {Type: "string"},
			"source_type":      {Type: "string"},
			"created_at":       {Type: "string", Format: "date-time"},
			"valid_from":       {Type: "string", Format: "date-time"},
			"valid_to":         {Type: "string", Format: "date-time"},
			"conversation_id":  {Type: "string"},
			"message_id":       {Type: "string"},
			"extracted_from":   {Type: "string"},
			"degree":           {Type: "integer"},
			"priority":         {Type: "integer"},
		},
		Required: []string{"id", "category", "content"},
	}
}

func edgeSchema() *Schema {
	return &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"source_id":     {Type: "string"},
			"target_id":     {Type: "string"},
			"relation_type": {Type: "string"},
			"weight":        {Type: "number", Format: "float"},
		},
		Required: []string{"source_id", "target_id", "relation_type"},
	}
}

func retrievalResultSchema() *Schema {
	return &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"seed_nodes":   {Type: "array", Items: &Schema{Ref: "#/components/schemas/GraphNode"}},
			"world_facts":  {Type: "array", Items: &Schema{Ref: "#/components/schemas/RetrievedFact"}},
			"opinions":     {Type: "array", Items: &Schema{Ref: "#/components/schemas/RetrievedFact"}},
			"experiences":  {Type: "array", Items: &Schema{Ref: "#/components/schemas/RetrievedFact"}},
			"observations": {Type: "array", Items: &Schema{Ref: "#/components/schemas/RetrievedFact"}},
		},
	}
}

func buildPaths() map[string]*PathItem {
	return map[string]*PathItem{
		"/health": {
			Get: &Operation{
				Summary:     "Liveness check",
				Description: "Basic liveness check — no DB hit beyond the open connection. Always returns 200.",
				OperationID: "health",
				Tags:        []string{"health"},
				Responses: map[string]Response{
					"200": {Description: "Healthy", Content: map[string]MediaType{
						"application/json": {Schema: ref("HealthResponse")},
					}},
				},
			},
		},
		"/health/live": {
			Get: &Operation{
				Summary:     "Kubernetes liveness probe",
				OperationID: "healthLive",
				Tags:        []string{"health"},
				Responses: map[string]Response{
					"200": {Description: "Healthy"},
				},
			},
		},
		"/health/ready": {
			Get: &Operation{
				Summary:     "Readiness probe",
				Description: "Runs dependency checks (DB, vector index, embedder, LLM extractor, disk). Returns 503 on critical failure.",
				OperationID: "healthReady",
				Tags:        []string{"health"},
				Responses: map[string]Response{
					"200": {Description: "Ready", Content: map[string]MediaType{
						"application/json": {Schema: ref("ReadyResponse")},
					}},
					"503": {Description: "Degraded"},
				},
			},
		},
		"/health/startup": {
			Get: &Operation{
				Summary:     "Startup probe",
				OperationID: "healthStartup",
				Tags:        []string{"health"},
				Responses: map[string]Response{
					"200": {Description: "Started"},
				},
			},
		},
		"/metrics": {
			Get: &Operation{
				Summary:     "Prometheus metrics",
				OperationID: "metrics",
				Tags:        []string{"health"},
				Responses: map[string]Response{
					"200": {Description: "Metrics in Prometheus exposition format"},
				},
			},
		},
		"/store": {
			Post: &Operation{
				Summary:     "Store an entity",
				Description: "Upsert an entity. Embedding is computed automatically if omitted.",
				OperationID: "store",
				Tags:        []string{"memory"},
				RequestBody: jsonBody("StoreRequest"),
				Responses: map[string]Response{
					"200": {Description: "Stored", Content: map[string]MediaType{
						"application/json": {Example: map[string]string{"status": "ok"}},
					}},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
					"422": errorResponse("Invalid input"),
				},
				Security: auth(),
			},
		},
		"/search": {
			Post: &Operation{
				Summary:     "Vector similarity search",
				Description: "Find entities most similar to the query by cosine similarity.",
				OperationID: "search",
				Tags:        []string{"memory"},
				RequestBody: jsonBody("SearchRequest"),
				Responses: map[string]Response{
					"200": {Description: "Results", Content: map[string]MediaType{
						"application/json": {Schema: &Schema{Type: "array", Items: ref("SearchResult")}},
					}},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/retrieve": {
			Post: &Operation{
				Summary:     "Graph walk retrieval",
				Description: "Walk the graph from seed entities and return ranked results.",
				OperationID: "retrieve",
				Tags:        []string{"memory"},
				RequestBody: jsonBody("RetrieveRequest"),
				Responses: map[string]Response{
					"200": {Description: "Retrieval result", Content: map[string]MediaType{
						"application/json": {Schema: ref("RetrievalResult")},
					}},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/query": {
			Post: &Operation{
				Summary:     "Full pipeline query",
				Description: "Embed → search → graph walk → markdown context.",
				OperationID: "query",
				Tags:        []string{"memory"},
				RequestBody: jsonBody("SearchRequest"),
				Responses: map[string]Response{
					"200": {Description: "Markdown context", Content: map[string]MediaType{
						"application/json": {Schema: ref("QueryResponse")},
					}},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/query/explain": {
			Post: &Operation{
				Summary:     "Query with score breakdown",
				Description: "Full pipeline with per-fact explainability (ScoreBreakdown).",
				OperationID: "queryExplain",
				Tags:        []string{"memory"},
				RequestBody: jsonBody("SearchRequest"),
				Responses: map[string]Response{
					"200": {Description: "Retrieval result with score breakdown", Content: map[string]MediaType{
						"application/json": {Schema: ref("RetrievalResult")},
					}},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/query/temporal": {
			Post: &Operation{
				Summary:     "Time-windowed query",
				Description: "Full pipeline filtered by time range (RFC3339).",
				OperationID: "queryTemporal",
				Tags:        []string{"memory", "temporal"},
				RequestBody: &RequestBody{
					Required: true,
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"query":     {Type: "string"},
									"top_k":     {Type: "integer", Default: 5},
									"time_from": {Type: "string", Format: "date-time"},
									"time_to":   {Type: "string", Format: "date-time"},
								},
								Required: []string{"query"},
							},
						},
					},
				},
				Responses: map[string]Response{
					"200": {Description: "Retrieval result", Content: map[string]MediaType{
						"application/json": {Schema: ref("RetrievalResult")},
					}},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/response": {
			Post: &Operation{
				Summary:     "Full pipeline with LLM response",
				Description: "Full pipeline + LLM-generated response.",
				OperationID: "response",
				Tags:        []string{"memory"},
				RequestBody: jsonBody("SearchRequest"),
				Responses: map[string]Response{
					"200": {Description: "Response", Content: map[string]MediaType{
						"application/json": {Schema: ref("ResponseResult")},
					}},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/edge": {
			Post: &Operation{
				Summary:     "Create an edge",
				Description: "Add a typed edge between two entities. Set auto_create=true to create missing entities.",
				OperationID: "createEdge",
				Tags:        []string{"memory"},
				RequestBody: jsonBody("EdgeRequest"),
				Responses: map[string]Response{
					"200": {Description: "Created"},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
					"422": errorResponse("Invalid input"),
				},
				Security: auth(),
			},
		},
		"/ingest": {
			Post: &Operation{
				Summary:     "Ingest dialog",
				Description: "Extract entities from dialog text, deduplicate, and store.",
				OperationID: "ingest",
				Tags:        []string{"ingest"},
				RequestBody: jsonBody("IngestRequest"),
				Responses: map[string]Response{
					"200": {Description: "Ingested"},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/task/status": {
			Post: &Operation{
				Summary:     "Update task status",
				Description: "Transition a task entity to a new lifecycle state.",
				OperationID: "taskStatus",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskStatusRequest"),
				Responses: map[string]Response{
					"204": {Description: "Updated"},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
					"422": errorResponse("Invalid state transition"),
				},
				Security: auth(),
			},
		},
		"/task/executable": {
			Post: &Operation{
				Summary:     "List executable tasks",
				Description: "Pending tasks whose blocked_by dependencies are all completed.",
				OperationID: "taskExecutable",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskListRequest"),
				Responses: map[string]Response{
					"200": {Description: "Tasks", Content: map[string]MediaType{
						"application/json": {Schema: ref("TaskExecutableResponse")},
					}},
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/task/next": {
			Post: &Operation{
				Summary:     "Alias for executable",
				OperationID: "taskNext",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskListRequest"),
				Responses: map[string]Response{
					"200": {Description: "Tasks", Content: map[string]MediaType{
						"application/json": {Schema: ref("TaskExecutableResponse")},
					}},
				},
				Security: auth(),
			},
		},
		"/task/list": {
			Post: &Operation{
				Summary:     "List tasks",
				Description: "Filter by status and/or goal_id.",
				OperationID: "taskList",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskListRequest"),
				Responses: map[string]Response{
					"200": {Description: "Tasks", Content: map[string]MediaType{
						"application/json": {Schema: ref("TaskExecutableResponse")},
					}},
				},
				Security: auth(),
			},
		},
		"/task/show": {
			Post: &Operation{
				Summary:     "Show task details",
				Description: "Returns task entity plus blocked_by and recovers_via edges.",
				OperationID: "taskShow",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskShowRequest"),
				Responses: map[string]Response{
					"200": {Description: "Task details", Content: map[string]MediaType{
						"application/json": {Schema: ref("TaskShowResponse")},
					}},
					"400": errorResponse("Bad request"),
				},
				Security: auth(),
			},
		},
		"/task/dep": {
			Post: &Operation{
				Summary:     "Manage task dependencies",
				Description: "Add or remove a dependency edge between tasks.",
				OperationID: "taskDep",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskDepRequest"),
				Responses: map[string]Response{
					"200": {Description: "Updated"},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/task/tree": {
			Post: &Operation{
				Summary:     "Task dependency tree",
				Description: "ASCII rendering of the task dependency tree.",
				OperationID: "taskTree",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskTreeRequest"),
				Responses: map[string]Response{
					"200": {Description: "Tree", Content: map[string]MediaType{
						"application/json": {Schema: ref("TaskTreeResponse")},
					}},
				},
				Security: auth(),
			},
		},
		"/task/create": {
			Post: &Operation{
				Summary:     "Create a task",
				Description: "Create a new task entity, optionally linked to context entities.",
				OperationID: "taskCreate",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskCreateRequest"),
				Responses: map[string]Response{
					"200": {Description: "Created", Content: map[string]MediaType{
						"application/json": {Schema: ref("TaskCreateResponse")},
					}},
					"400": errorResponse("Bad request"),
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/task/rollback": {
			Post: &Operation{
				Summary:     "Find rollback task",
				Description: "Find the task linked via recovers_via from a failed task.",
				OperationID: "taskRollback",
				Tags:        []string{"task"},
				RequestBody: jsonBody("TaskRollbackRequest"),
				Responses: map[string]Response{
					"200": {Description: "Rollback task", Content: map[string]MediaType{
						"application/json": {Schema: ref("TaskRollbackResponse")},
					}},
					"400": errorResponse("Bad request"),
				},
				Security: auth(),
			},
		},
		"/timeline": {
			Get: &Operation{
				Summary:     "Recent entities",
				Description: "Entities ordered by created_at DESC.",
				OperationID: "timeline",
				Tags:        []string{"temporal"},
				Parameters: []Parameter{
					{Name: "limit", In: "query", Schema: &Schema{Type: "integer", Default: 50}},
				},
				Responses: map[string]Response{
					"200": {Description: "Timeline", Content: map[string]MediaType{
						"application/json": {Schema: &Schema{Type: "array", Items: ref("TimelineEntry")}},
					}},
				},
			},
		},
		"/contradictions": {
			Get: &Operation{
				Summary:     "List contradictions",
				Description: "All contradicts edges. Optional ?id=X filters to specific entity.",
				OperationID: "contradictions",
				Tags:        []string{"graph"},
				Parameters: []Parameter{
					{Name: "id", In: "query", Schema: &Schema{Type: "string"}},
				},
				Responses: map[string]Response{
					"200": {Description: "Contradiction pairs", Content: map[string]MediaType{
						"application/json": {Schema: &Schema{Type: "array", Items: ref("ContradictionPair")}},
					}},
				},
			},
		},
		"/connected-components": {
			Get: &Operation{
				Summary:     "Connected components",
				Description: "BFS-based connected components in the graph.",
				OperationID: "connectedComponents",
				Tags:        []string{"graph"},
				Parameters: []Parameter{
					{Name: "min_size", In: "query", Schema: &Schema{Type: "integer", Default: 2}},
				},
				Responses: map[string]Response{
					"200": {Description: "Components", Content: map[string]MediaType{
						"application/json": {Schema: &Schema{Type: "array", Items: ref("ConnectedComponent")}},
					}},
				},
			},
		},
		"/communities": {
			Get: &Operation{
				Summary:     "Community detection",
				Description: "Louvain community detection with modularity scoring.",
				OperationID: "communities",
				Tags:        []string{"graph"},
				Parameters: []Parameter{
					{Name: "min_size", In: "query", Schema: &Schema{Type: "integer"}},
					{Name: "max_iterations", In: "query", Schema: &Schema{Type: "integer"}},
				},
				Responses: map[string]Response{
					"200": {Description: "Communities", Content: map[string]MediaType{
						"application/json": {Schema: &Schema{
							Type: "object",
							Properties: map[string]*Schema{
								"communities":       {Type: "array", Items: ref("Community")},
								"global_modularity": {Type: "number", Format: "double"},
								"total_communities": {Type: "integer"},
							},
						}},
					}},
				},
			},
		},
		"/graph/verify": {
			Get: &Operation{
				Summary:     "Graph integrity check",
				Description: "Checks entities, edges, embeddings, orphan edges, invalid types.",
				OperationID: "graphVerify",
				Tags:        []string{"graph"},
				Responses: map[string]Response{
					"200": {Description: "Verify report", Content: map[string]MediaType{
						"application/json": {Schema: ref("VerifyReport")},
					}},
				},
			},
		},
		"/provenance": {
			Get: &Operation{
				Summary:     "Query by provenance",
				Description: "Filter entities by memory origin (conversation_id, message_id, source).",
				OperationID: "provenance",
				Tags:        []string{"graph"},
				Parameters: []Parameter{
					{Name: "conversation_id", In: "query", Schema: &Schema{Type: "string"}},
					{Name: "message_id", In: "query", Schema: &Schema{Type: "string"}},
					{Name: "source", In: "query", Schema: &Schema{Type: "string"}},
					{Name: "limit", In: "query", Schema: &Schema{Type: "integer"}},
				},
				Responses: map[string]Response{
					"200": {Description: "Entities", Content: map[string]MediaType{
						"application/json": {Schema: &Schema{Type: "array", Items: ref("Entity")}},
					}},
				},
			},
		},
		"/recovery-plan": {
			Get: &Operation{
				Summary:     "Recovery plan",
				Description: "Walk recovers_via chain from a failed task.",
				OperationID: "recoveryPlan",
				Tags:        []string{"task"},
				Parameters: []Parameter{
					{Name: "id", In: "query", Required: true, Schema: &Schema{Type: "string"}},
				},
				Responses: map[string]Response{
					"200": {Description: "Recovery chain", Content: map[string]MediaType{
						"application/json": {Schema: &Schema{Type: "array", Items: ref("Entity")}},
					}},
				},
			},
		},
		"/admin/re-embed": {
			Post: &Operation{
				Summary:     "Re-embed all entities",
				Description: "Batch re-embed all entities after model/dimension change.",
				OperationID: "reEmbed",
				Tags:        []string{"admin"},
				RequestBody: jsonBody("ReEmbedRequest"),
				Responses: map[string]Response{
					"200": {Description: "Result", Content: map[string]MediaType{
						"application/json": {Schema: ref("ReEmbedResult")},
					}},
					"401": errorResponse("Unauthorized"),
				},
				Security: auth(),
			},
		},
		"/db/migrate": {
			Get: &Operation{
				Summary:     "Migration status",
				Description: "Shows applied and pending migrations.",
				OperationID: "dbMigrate",
				Tags:        []string{"admin"},
				Responses: map[string]Response{
					"200": {Description: "Migration list", Content: map[string]MediaType{
						"application/json": {Schema: &Schema{Type: "array", Items: ref("MigStatus")}},
					}},
				},
			},
		},
		"/db/rollback": {
			Post: &Operation{
				Summary:     "Roll back migration",
				Description: "Roll back the most recently applied migration.",
				OperationID: "dbRollback",
				Tags:        []string{"admin"},
				Responses: map[string]Response{
					"200": {Description: "Rolled back"},
				},
			},
		},
		"/db/verify": {
			Get: &Operation{
				Summary:     "Schema integrity",
				Description: "Checksum integrity check across all migrations.",
				OperationID: "dbVerify",
				Tags:        []string{"admin"},
				Responses: map[string]Response{
					"200": {Description: "Result"},
				},
			},
		},
		"/db/schema": {
			Get: &Operation{
				Summary:     "Schema fingerprint",
				Description: "Stored vs current schema fingerprint comparison.",
				OperationID: "dbSchema",
				Tags:        []string{"admin"},
				Responses: map[string]Response{
					"200": {Description: "Schema report", Content: map[string]MediaType{
						"application/json": {Schema: ref("SchemaReport")},
					}},
				},
			},
		},
	}
}

// MarshalJSON returns the spec as JSON bytes.
func (s *Spec) MarshalJSON() ([]byte, error) {
	type specAlias Spec
	return json.MarshalIndent((*specAlias)(s), "", "  ")
}

// MarshalYAML returns the spec as YAML bytes.
func (s *Spec) MarshalYAML() ([]byte, error) {
	type specAlias Spec
	return yaml.Marshal((*specAlias)(s))
}

// JSON returns the spec as indented JSON.
func (s *Spec) JSON() []byte {
	b, _ := s.MarshalJSON()
	return b
}

// YAMLBytes returns the spec as YAML.
func (s *Spec) YAMLBytes() []byte {
	b, _ := s.MarshalYAML()
	return b
}

func ref(name string) *Schema {
	return &Schema{Ref: "#/components/schemas/" + name}
}

func jsonBody(schema string) *RequestBody {
	return &RequestBody{
		Required: true,
		Content: map[string]MediaType{
			"application/json": {Schema: ref(schema)},
		},
	}
}

func errorResponse(desc string) Response {
	return Response{
		Description: desc,
		Content: map[string]MediaType{
			"application/json": {Schema: ref("ErrorResponse")},
		},
	}
}

func auth() []SecurityRequirement {
	return []SecurityRequirement{{"ApiKeyAuth": {}}}
}
