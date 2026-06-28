package hermem

import "context"

// MemoryClient handles memory operations (store, search, query, etc.).
type MemoryClient struct {
	c *Client
}

// Store upserts an entity. Embedding is computed automatically if omitted.
func (m *MemoryClient) Store(ctx context.Context, req *StoreRequest) error {
	return m.c.doNoContent(ctx, "POST", "/store", req)
}

// Search returns the top-K entities by cosine similarity.
func (m *MemoryClient) Search(ctx context.Context, req *SearchRequest) ([]SearchResult, error) {
	var result []SearchResult
	err := m.c.do(ctx, "POST", "/search", req, &result)
	return result, err
}

// Retrieve walks the graph from seed entities.
func (m *MemoryClient) Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrievalResult, error) {
	var result RetrievalResult
	err := m.c.do(ctx, "POST", "/retrieve", req, &result)
	return &result, err
}

// Query runs the full pipeline: embed → search → graph walk → markdown.
func (m *MemoryClient) Query(ctx context.Context, req *SearchRequest) (*QueryResponse, error) {
	var result QueryResponse
	err := m.c.do(ctx, "POST", "/query", req, &result)
	return &result, err
}

// Explain runs the full pipeline with per-fact score breakdown.
func (m *MemoryClient) Explain(ctx context.Context, req *SearchRequest) (*RetrievalResult, error) {
	var result RetrievalResult
	err := m.c.do(ctx, "POST", "/query/explain", req, &result)
	return &result, err
}

// Ingest extracts entities from dialog text.
func (m *MemoryClient) Ingest(ctx context.Context, req *IngestRequest) error {
	return m.c.doNoContent(ctx, "POST", "/ingest", req)
}

// Edge creates a typed edge between two entities.
func (m *MemoryClient) Edge(ctx context.Context, req *EdgeRequest) error {
	return m.c.doNoContent(ctx, "POST", "/edge", req)
}

// ReEmbed triggers batch re-embedding of all entities.
func (m *MemoryClient) ReEmbed(ctx context.Context, req *ReEmbedRequest) (*ReEmbedResult, error) {
	var result ReEmbedResult
	err := m.c.do(ctx, "POST", "/admin/re-embed", req, &result)
	return &result, err
}
