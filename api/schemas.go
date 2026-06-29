package api

// AllSchemas returns all reusable component schemas.
func AllSchemas() map[string]*Schema {
	return map[string]*Schema{
		"Entity":  entitySchema(),
		"Edge":    edgeSchema(),
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
