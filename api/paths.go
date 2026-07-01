package api

// AllPaths returns all API path definitions.
func AllPaths() map[string]*PathItem {
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
		"/task/claim-next": {
			Post: &Operation{
				Summary:     "Claim next executable task",
				Description: "Atomically claims and returns the next executable task for processing.",
				OperationID: "taskClaimNext",
				Tags:        []string{"task"},
				Responses: map[string]Response{
					"200": {Description: "Claimed task", Content: map[string]MediaType{
						"application/json": {Schema: ref("TaskExecutableResponse")},
					}},
					"404": errorResponse("No executable tasks"),
				},
				Security: auth(),
			},
		},
		"/ingest/jobs": {
			Get: &Operation{
				Summary:     "List ingest jobs",
				Description: "Returns the status of recent ingestion jobs.",
				OperationID: "ingestJobs",
				Tags:        []string{"ingest"},
				Responses: map[string]Response{
					"200": {Description: "Job list"},
				},
			},
		},
		"/admin/retention/run": {
			Post: &Operation{
				Summary:     "Run retention sweep",
				Description: "Trigger a retention sweep to archive stale entities.",
				OperationID: "retentionRun",
				Tags:        []string{"admin"},
				Responses: map[string]Response{
					"200": {Description: "Retention sweep completed"},
				},
				Security: auth(),
			},
		},
	}
}
