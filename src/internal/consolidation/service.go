// Package consolidation owns the transport-agnostic semantic memory
// consolidation ("graph sleep cycle"): clustering redundant
// observations/facts, synthesizing high-order insights via the LLM
// extractor, and archiving source nodes while preserving provenance.
package consolidation

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/extraction"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Service is the transport-agnostic consolidation orchestrator.
type Service struct {
	db        *sql.DB
	vi        spi.VectorStore
	embedder  spi.Embedder
	extractor extraction.LLMExtractor
}

// New constructs a Service. Extra deps (embedder, extractor) may be
// passed positionally or omitted; missing deps degrade gracefully.
func New(db *sql.DB, vi spi.VectorStore, deps ...interface{}) *Service {
	s := &Service{db: db, vi: vi}
	for _, d := range deps {
		switch v := d.(type) {
		case spi.Embedder:
			s.embedder = v
		case extraction.LLMExtractor:
			s.extractor = v
		}
	}
	return s
}

// NewService is an alias for New.
func NewService(db *sql.DB, vi spi.VectorStore, deps ...interface{}) *Service {
	return New(db, vi, deps...)
}

func (s *Service) Run(ctx context.Context, policy Policy) {
	if policy.Interval <= 0 {
		policy.Interval = 24 * time.Hour
	}
	if policy.ClusterSize <= 0 {
		policy.ClusterSize = 20
	}
	if policy.DedupThreshold <= 0 {
		policy.DedupThreshold = 0.88
	}
	ticker := time.NewTicker(policy.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RunOnce(ctx, policy); err != nil {
				slog.Error("consolidation run", "err", err)
			}
		}
	}
}

// Consolidate is a one-shot alias for RunOnce using the default policy.
func (s *Service) Consolidate(ctx context.Context, policies ...Policy) (Report, error) {
	pol := Default()
	if len(policies) > 0 {
		pol = policies[0]
	}
	if pol.Interval <= 0 {
		pol.Interval = 24 * time.Hour
	}
	if pol.ClusterSize <= 0 {
		pol.ClusterSize = 20
	}
	if pol.DedupThreshold <= 0 {
		pol.DedupThreshold = 0.88
	}
	return s.RunOnce(ctx, pol)
}

// RunOnce performs a single consolidation pass.
func (s *Service) RunOnce(ctx context.Context, policy Policy) (rep Report, err error) {
	rep.StartedAt = time.Now()
	defer func() { rep.FinishedAt = time.Now(); rep.DurationMs = rep.FinishedAt.Sub(rep.StartedAt).Milliseconds() }()

	if policy.ClusterSize <= 0 {
		policy.ClusterSize = 20
	}
	if policy.DedupThreshold <= 0 {
		policy.DedupThreshold = 0.88
	}

	// Load candidate entities (low-order: observation + fact).
	rows, qerr := s.db.QueryContext(ctx,
		`SELECT id, category, content, embedding FROM entities WHERE archived = 0 AND category IN ('observation','fact')`)
	if qerr != nil {
		rep.Error = qerr.Error()
		err = fmt.Errorf("consolidation: select: %w", qerr)
		return
	}
	defer rows.Close()

	type cand struct {
		id        string
		category  string
		content   string
		embedding []float32
	}
	var cands []cand
	for rows.Next() {
		var c cand
		var emb []byte
		if serr := rows.Scan(&c.id, &c.category, &c.content, &emb); serr != nil {
			rep.Error = serr.Error()
			err = fmt.Errorf("consolidation: scan: %w", serr)
			return
		}
		c.embedding = bytesToFloats(emb)
		cands = append(cands, c)
	}
	if rerr := rows.Err(); rerr != nil {
		rep.Error = rerr.Error()
		err = fmt.Errorf("consolidation: rows: %w", rerr)
		return
	}

	if len(cands) < 2 {
		return
	}

	// Convert to domain entities for clustering.
	ents := make([]domain.Entity, len(cands))
	for i, c := range cands {
		ents[i] = domain.Entity{ID: c.id, Category: c.category, Content: c.content, Embedding: c.embedding}
	}

	// Load edges among candidates for graph-adjacency clustering.
	edgeSet := map[string]bool{}
	erows, eerr := s.db.QueryContext(ctx, `SELECT source_id, target_id FROM edges`)
	if eerr == nil {
		defer erows.Close()
		for erows.Next() {
			var src, dst string
			if err := erows.Scan(&src, &dst); err == nil {
				edgeSet[src+"->"+dst] = true
				edgeSet[dst+"->"+src] = true
			}
		}
	}

	clusters := ClusterEntitiesWithEdges(ents, policy.DedupThreshold, edgeSet)
	// Only clusters with >= 2 members are actionable.
	var actionable [][]domain.Entity
	for _, cl := range clusters {
		if len(cl) >= 2 {
			actionable = append(actionable, cl)
		}
	}
	if len(actionable) == 0 {
		return
	}

	// Count clustered nodes.
	for _, cl := range actionable {
		rep.NodesClustered += len(cl)
	}

	if s.extractor == nil {
		// No synthesis possible; still report clustered count.
		return
	}

	for _, cl := range actionable {
		// Split into chunks of ClusterSize.
		for start := 0; start < len(cl); start += policy.ClusterSize {
			end := start + policy.ClusterSize
			if end > len(cl) {
				end = len(cl)
			}
			chunk := cl[start:end]
			if len(chunk) < 2 {
				continue
			}
			// Build prompt.
			prompt := BuildSynthesisPrompt(chunk)
			res, xerr := s.extractor.ExtractEntities(ctx, prompt)
			if xerr != nil {
				slog.Warn("consolidation extract failed", "err", xerr)
				continue
			}
			if res == nil || len(res.Entities) == 0 {
				continue
			}
			syn := res.Entities[0]
			cat := syn.Category
			if cat == "" || !allowedConsolidationCategory(cat) {
				cat = "fact"
			}
			content := syn.Content
			if content == "" {
				// Fallback: concatenate.
				var b strings.Builder
				for _, e := range chunk {
					b.WriteString(e.Content)
					b.WriteString(" ")
				}
				content = strings.TrimSpace(b.String())
			}

			// Embed content.
			var emb []float32
			if s.embedder != nil {
				if e, err := s.embedder.Embed(ctx, content); err == nil {
					emb = e
				}
			}

			newID := fmt.Sprintf("consolidated-%d", time.Now().UnixNano())

			// Persist in transaction.
			tx, berr := s.db.BeginTx(ctx, nil)
			if berr != nil {
				slog.Warn("consolidation begin tx", "err", berr)
				continue
			}
			// Insert new entity.
			embBytes := store.EmbeddingToBytes(emb)
			_, ierr := tx.ExecContext(ctx, `INSERT INTO entities (id, category, content, embedding, updated_at, archived) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 0)`,
				newID, cat, content, embBytes)
			if ierr != nil {
				tx.Rollback()
				slog.Warn("consolidation insert", "err", ierr)
				continue
			}
			// Add extracted_from edges.
			for _, src := range chunk {
				_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, 'extracted_from', 1.0)`,
					newID, src.ID)
			}
			// Archive sources.
			ids := make([]interface{}, len(chunk))
			phs := make([]string, len(chunk))
			for i, e := range chunk {
				phs[i] = "?"
				ids[i] = e.ID
			}
			_, _ = tx.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id IN (`+strings.Join(phs, ",")+`)`, ids...)
			if cerr := tx.Commit(); cerr != nil {
				tx.Rollback()
				slog.Warn("consolidation commit", "err", cerr)
				continue
			}

			// Vector index updates.
			if s.vi != nil {
				if len(emb) > 0 {
					_ = s.vi.Upsert(ctx, []spi.VectorRecord{{Namespace: spi.DefaultNamespace, ID: newID, Vector: emb}})
				}
				srcIDs := make([]string, len(chunk))
				for i, e := range chunk {
					srcIDs[i] = e.ID
				}
				_ = s.vi.Delete(ctx, spi.DeleteRequest{Namespace: spi.DefaultNamespace, IDs: srcIDs})
			}

			rep.NodesSynthesized++
			rep.NodesArchived += len(chunk)
		}
	}

	return
}

func allowedConsolidationCategory(cat string) bool {
	return cat == "fact" || cat == "belief" || cat == "world" || cat == "summary"
}

// ClusterEntities groups entities into clusters using vector proximity.
func ClusterEntities(entities []domain.Entity, threshold float32) [][]domain.Entity {
	return ClusterEntitiesWithEdges(entities, threshold, nil)
}

// ClusterEntitiesWithEdges groups entities using vector proximity and graph edges.
func ClusterEntitiesWithEdges(entities []domain.Entity, threshold float32, edges map[string]bool) [][]domain.Entity {
	n := len(entities)
	if n == 0 {
		return nil
	}
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if edges != nil && edges[entities[i].ID+"->"+entities[j].ID] {
				union(i, j)
				continue
			}
			if len(entities[i].Embedding) > 0 && len(entities[j].Embedding) > 0 {
				if cosineSimilarity(entities[i].Embedding, entities[j].Embedding) >= float64(threshold) {
					union(i, j)
				}
			}
		}
	}
	groups := map[int][]domain.Entity{}
	for i := 0; i < n; i++ {
		r := find(i)
		groups[r] = append(groups[r], entities[i])
	}
	out := make([][]domain.Entity, 0, len(groups))
	for _, g := range groups {
		if len(g) >= 2 {
			out = append(out, g)
		}
	}
	// Sort by size desc for determinism.
	sort.Slice(out, func(a, b int) bool { return len(out[a]) > len(out[b]) })
	return out
}

// Aliases for clustering function names.
var (
	Cluster        = ClusterEntities
	ClusterNodes   = ClusterEntities
	FindClusters   = ClusterEntities
	DetectClusters = ClusterEntities
)

// BuildSynthesisPrompt builds the LLM prompt for a cluster.
func BuildSynthesisPrompt(cluster []domain.Entity) string {
	var b strings.Builder
	b.WriteString("Consolidate the following observations into a single concise fact:\n")
	for _, e := range cluster {
		b.WriteString("- ")
		b.WriteString(e.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// Aliases for payload generation.
func BuildSynthesisPayload(cluster []domain.Entity) string { return BuildSynthesisPrompt(cluster) }
func SynthesisPrompt(cluster []domain.Entity) string       { return BuildSynthesisPrompt(cluster) }
func SynthesisPayload(cluster []domain.Entity) string      { return BuildSynthesisPrompt(cluster) }

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func bytesToFloats(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	// reuse store? store has BytesToEmbedding maybe.
	return store.BytesToEmbedding(b)
}

// Stubs for metric/compat? none.
var _ = slog.Info
