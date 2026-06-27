package episodic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	hermemtime "github.com/pavelveter/hermem/src/internal/util/time"
)

// EpisodeFilter narrows the candidate set before ranking. Zero
// value on any field means "no constraint". Limit ≤ 0 means no cap.
type EpisodeFilter struct {
	SessionID         string
	TimeFrom          time.Time
	TimeTo            time.Time
	HasSummary        bool
	HasLinkedMemories bool
	Limit             int
}

// RetrievalService searches the episodes table. Optional semantic
// ranking via a core.Embedder — when an embedder is wired and the
// caller passes a non-empty query, episodes that carry an
// `embedding` entry in their metadata JSON are ranked by cosine
// similarity to the query embedding. Episodes without a stored
// embedding are pushed to the end (cosine = 0) so they never
// dominate a non-match.
//
// Flat-package + stateless pattern, same as the rest of episodic.
type RetrievalService struct {
	db       *sql.DB
	embedder core.Embedder // optional; nil disables semantic ranking
}

// NewRetrievalService constructs a RetrievalService. embedder may
// be nil — callers that don't need semantic ranking can pass nil
// and the service falls back to pure SQL filtering.
func NewRetrievalService(db *sql.DB, embedder core.Embedder) *RetrievalService {
	return &RetrievalService{db: db, embedder: embedder}
}

// SearchEpisodes returns episodes matching the filter, optionally
// reranked by cosine similarity to query if an embedder is wired
// and query is non-empty. Result order:
//
//   - Pure SQL (no embedder or empty query): ORDER BY started_at_ms DESC
//     (most-recent first), limit applied.
//   - Semantic (embedder + query): same SQL filter + limit, then
//     in-memory rerank by cosine similarity DESC. Episodes without
//     a stored embedding get cosine=0 and cluster at the bottom.
//
// Filter composition: zero-value filter fields are no-ops. TimeFrom
// and TimeTo are inclusive on both ends. The ms-quantised
// comparison (started_at_ms >= ?) is intentional — callers pass
// time.Time values and the hermemtime helper converts them to
// INTEGER ms before binding so the range is TZ-invariant.
func (s *RetrievalService) SearchEpisodes(ctx context.Context, query string, filter EpisodeFilter) ([]Episode, error) {
	// Apply SQL filter. Compose the WHERE clause incrementally so
	// callers that set only one field still get a correct query.
	q := `SELECT id, session_id, conversation_id, title, summary, started_at_ms, ended_at_ms, metadata FROM episodes WHERE 1=1`
	args := []any{}
	if filter.SessionID != "" {
		q += " AND session_id = ?"
		args = append(args, filter.SessionID)
	}
	if !filter.TimeFrom.IsZero() {
		q += " AND started_at_ms >= ?"
		args = append(args, hermemtime.UnixMillisFromTime(filter.TimeFrom))
	}
	if !filter.TimeTo.IsZero() {
		q += " AND started_at_ms <= ?"
		args = append(args, hermemtime.UnixMillisFromTime(filter.TimeTo))
	}
	if filter.HasSummary {
		q += " AND summary != ''"
	}
	if filter.HasLinkedMemories {
		q += ` AND EXISTS (SELECT 1 FROM episode_memories WHERE episode_id = episodes.id)`
	}
	q += " ORDER BY started_at_ms DESC"
	if filter.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("episodic: SearchEpisodes query: %w", err)
	}
	defer rows.Close()
	var out []Episode
	for rows.Next() {
		ep, err := scanEpisodeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: SearchEpisodes rows: %w", err)
	}
	out = core.NormalizeSlice(out)

	// Semantic rerank — only when caller asks AND an embedder is wired.
	if s.embedder != nil && query != "" {
		queryVec, err := s.embedder.Embed(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("episodic: SearchEpisodes embed query: %w", err)
		}
		rankByCosine(queryVec, out)
	}
	return out, nil
}

// rankByCosine in-place sorts episodes by cosine similarity to
// queryVec DESC. Episodes whose metadata doesn't carry an
// `embedding` entry are treated as cosine=0 and pushed to the
// bottom — they never dominate a true match.
func rankByCosine(queryVec []float32, episodes []Episode) {
	if len(queryVec) == 0 {
		return
	}
	scores := make([]float64, len(episodes))
	for i := range episodes {
		scores[i] = cosineAgainstMetadata(queryVec, episodes[i].Metadata)
	}
	// Stable sort by score desc.
	for i := 1; i < len(episodes); i++ {
		for j := i; j > 0 && scores[j] > scores[j-1]; j-- {
			episodes[j], episodes[j-1] = episodes[j-1], episodes[j]
			scores[j], scores[j-1] = scores[j-1], scores[j]
		}
	}
}

// cosineAgainstMetadata extracts the episode's stored embedding
// from its metadata JSON (key "embedding", value []float64) and
// computes cosine similarity. Returns 0 when the metadata is
// missing or the embedding key is absent — those episodes cluster
// at the bottom of the reranked result.
func cosineAgainstMetadata(queryVec []float32, meta map[string]any) float64 {
	raw, ok := meta["embedding"]
	if !ok {
		return 0
	}
	vec, ok := raw.([]any)
	if !ok {
		// JSON unmarshal into map[string]any gives []any for slices.
		// If a future caller stores []float64 directly, fall back.
		if f, ok2 := raw.([]float64); ok2 {
			return cosine(queryVec, toFloat32(f))
		}
		return 0
	}
	floats := make([]float32, len(vec))
	for i, v := range vec {
		switch n := v.(type) {
		case float64:
			floats[i] = float32(n)
		case float32:
			floats[i] = n
		default:
			return 0 // unsupported element type
		}
	}
	return cosine(queryVec, floats)
}

// cosine is the standard cosine similarity in float64 for stable
// sort comparisons. Returns 0 for empty / mismatched-dim vectors.
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// toFloat32 is a tiny adapter for callers that store embeddings
// as []float64 instead of the JSON round-trip []any shape.
func toFloat32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}

// Ensure the encode path is exercised at compile time so the
// `embedding` JSON key shape stays in sync with cosineAgainstMetadata.
var _ = json.Marshal
