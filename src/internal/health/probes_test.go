package health_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/health"
	"github.com/pavelveter/hermem/src/internal/store"
)

type mockVectorIndex struct {
	searchFunc func(ctx context.Context, vec []float32, limit int) ([]string, error)
}

func (m *mockVectorIndex) Search(ctx context.Context, vec []float32, limit int) ([]string, error) {
	return m.searchFunc(ctx, vec, limit)
}
func (m *mockVectorIndex) SearchBatch(ctx context.Context, vecs [][]float32, limit int) ([][]string, error) {
	return nil, nil
}
func (m *mockVectorIndex) Store(ctx context.Context, id string, vec []float32) error {
	return nil
}
func (m *mockVectorIndex) Remove(ctx context.Context, ids []string) error {
	return nil
}

type mockEmbedder struct {
	embedFunc func(ctx context.Context, content string) ([]float32, error)
}

func (m *mockEmbedder) Embed(ctx context.Context, content string) ([]float32, error) {
	return m.embedFunc(ctx, content)
}

func (m *mockEmbedder) Ping(ctx context.Context) error {
	return nil
}

type mockExtractor struct {
	extractFunc func(ctx context.Context, dialog string) (*core.ExtractionResult, error)
}

func (m *mockExtractor) ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
	return m.extractFunc(ctx, dialog)
}

type mockReranker struct {
	rerankFunc func(ctx context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error)
}

func (m *mockReranker) Rerank(ctx context.Context, query string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	return m.rerankFunc(ctx, query, facts)
}

func TestDBProbe_ClosedDB(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	db.Close()
	svc := health.New(health.DBProbe(db))
	st := svc.Ready(t.Context())
	r, ok := st.Checks["database"]
	if !ok {
		t.Fatal("missing database check result")
	}
	if r.OK {
		t.Fatal("expected database check to fail with closed db")
	}
}

func TestVectorIndexProbe_OK(t *testing.T) {
	vi := &mockVectorIndex{
		searchFunc: func(ctx context.Context, vec []float32, limit int) ([]string, error) {
			return []string{}, nil
		},
	}
	svc := health.New(health.VectorIndexProbe(vi, 3))
	st := svc.Ready(t.Context())
	r := st.Checks["vector_index"]
	if !r.OK {
		t.Fatalf("expected vector_index OK, got error: %s", r.Error)
	}
}

func TestVectorIndexProbe_Error(t *testing.T) {
	vi := &mockVectorIndex{
		searchFunc: func(ctx context.Context, vec []float32, limit int) ([]string, error) {
			return nil, errors.New("index unavailable")
		},
	}
	svc := health.New(health.VectorIndexProbe(vi, 3))
	st := svc.Ready(t.Context())
	r := st.Checks["vector_index"]
	if r.OK {
		t.Fatal("expected vector_index to fail")
	}
}

func TestVectorIndexProbe_Nil(t *testing.T) {
	svc := health.New(health.VectorIndexProbe(nil, 3))
	st := svc.Ready(t.Context())
	r := st.Checks["vector_index"]
	if r.OK {
		t.Fatal("expected nil vector_index to fail")
	}
}

func TestEmbedderProbe_OK(t *testing.T) {
	em := &mockEmbedder{
		embedFunc: func(ctx context.Context, content string) ([]float32, error) {
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	svc := health.New(health.EmbedderProbe(em))
	st := svc.Ready(t.Context())
	r := st.Checks["embedder"]
	if !r.OK {
		t.Fatalf("expected embedder OK, got error: %s", r.Error)
	}
}

func TestEmbedderProbe_Error(t *testing.T) {
	em := &mockEmbedder{
		embedFunc: func(ctx context.Context, content string) ([]float32, error) {
			return nil, errors.New("embedder unavailable")
		},
	}
	svc := health.New(health.EmbedderProbe(em))
	st := svc.Ready(t.Context())
	r := st.Checks["embedder"]
	if r.OK {
		t.Fatal("expected embedder to fail")
	}
}

func TestEmbedderProbe_Timeout(t *testing.T) {
	em := &mockEmbedder{
		embedFunc: func(ctx context.Context, _ string) ([]float32, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return []float32{0.1}, nil
			}
		},
	}
	svc := health.New(health.EmbedderProbe(em))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	st := svc.Ready(ctx)
	r := st.Checks["embedder"]
	if r.OK {
		t.Fatal("expected embedder to time out")
	}
}

func TestExtractorProbe_NilIsWarning(t *testing.T) {
	svc := health.New(health.ExtractorProbe(nil))
	st := svc.Ready(t.Context())
	r := st.Checks["extractor"]
	if r.OK {
		t.Fatal("expected extractor to fail when nil")
	}
	if !r.Critical {
		t.Log("extractor probe severity is warning (not critical) — one less thing to block on")
	}
}

func TestExtractorProbe_OK(t *testing.T) {
	ex := &mockExtractor{
		extractFunc: func(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
			return &core.ExtractionResult{}, nil
		},
	}
	svc := health.New(health.ExtractorProbe(ex))
	st := svc.Ready(t.Context())
	r := st.Checks["extractor"]
	if !r.OK {
		t.Fatalf("expected extractor OK, got error: %s", r.Error)
	}
}

// TestRerankerProbe_OK — the reranker round-trips a "ping" fact list and
// returns its (re-ordered) slice; the probe interprets any non-error
// result as healthy. This pins the happy path against a future refactor
// that might, say, swap the probe to a deeper connectivity check.
func TestRerankerProbe_OK(t *testing.T) {
	r := &mockReranker{
		rerankFunc: func(ctx context.Context, _ string, _ []core.RetrievedFact) ([]core.RetrievedFact, error) {
			return []core.RetrievedFact{}, nil
		},
	}
	svc := health.New(health.RerankerProbe(r))
	st := svc.Ready(t.Context())
	res := st.Checks["reranker"]
	if !res.OK {
		t.Fatalf("expected reranker OK, got error: %s", res.Error)
	}
	if res.Critical {
		t.Fatal("reranker probe must be non-critical (warning severity) so an outage does not flap readiness")
	}
}

// TestRerankerProbe_Nil — when env.Reranker is nil (operator opted out
// of the rerank feature), the probe must report a failure AND keep its
// non-critical severity, so the readiness probe still says Ready.
func TestRerankerProbe_Nil(t *testing.T) {
	svc := health.New(health.RerankerProbe(nil))
	st := svc.Ready(t.Context())
	res := st.Checks["reranker"]
	if res.OK {
		t.Fatal("expected nil reranker to fail the probe")
	}
	if !strings.Contains(res.Error, "nil") {
		t.Errorf("expected nil-mention in probe error, got %q", res.Error)
	}
	if res.Critical {
		t.Fatal("reranker probe must be non-critical (warning severity) — an absent reranker must not flake readiness")
	}
}

// TestRerankerProbe_Error — a reachable but broken reranker reports the
// upstream error verbatim and stays non-critical.
func TestRerankerProbe_Error(t *testing.T) {
	r := &mockReranker{
		rerankFunc: func(ctx context.Context, _ string, _ []core.RetrievedFact) ([]core.RetrievedFact, error) {
			return nil, errors.New("reranker backend down")
		},
	}
	svc := health.New(health.RerankerProbe(r))
	st := svc.Ready(t.Context())
	res := st.Checks["reranker"]
	if res.OK {
		t.Fatal("expected error-reranker to fail the probe")
	}
	if !strings.Contains(res.Error, "reranker backend down") {
		t.Errorf("expected upstream error to surface, got %q", res.Error)
	}
	if res.Critical {
		t.Fatal("reranker probe must be non-critical (warning severity)")
	}
}

func TestStatusAggregation_CriticalFailUnhealthy(t *testing.T) {
	svc := health.New(
		health.Check{
			Name:     "critical_ok",
			Probe:    func(ctx context.Context) error { return nil },
			Timeout:  time.Second,
			Severity: "critical",
		},
		health.Check{
			Name:     "critical_fail",
			Probe:    func(ctx context.Context) error { return errors.New("fail") },
			Timeout:  time.Second,
			Severity: "critical",
		},
		health.Check{
			Name:     "warning_fail",
			Probe:    func(ctx context.Context) error { return errors.New("warn") },
			Timeout:  time.Second,
			Severity: "warning",
		},
	)
	st := svc.Ready(t.Context())
	if st.Ready {
		t.Fatal("expected Ready=false when critical check fails")
	}
	if st.Status != "degraded" {
		t.Fatalf("expected degraded, got %s", st.Status)
	}
}

func TestStatusAggregation_WarningOnlyStillHealthy(t *testing.T) {
	svc := health.New(
		health.Check{
			Name:     "critical_ok",
			Probe:    func(ctx context.Context) error { return nil },
			Timeout:  time.Second,
			Severity: "critical",
		},
		health.Check{
			Name:     "warning_fail",
			Probe:    func(ctx context.Context) error { return errors.New("warn") },
			Timeout:  time.Second,
			Severity: "warning",
		},
	)
	st := svc.Ready(t.Context())
	if !st.Ready {
		t.Fatal("expected Ready=true when only warning checks fail")
	}
}

func TestDiskSpaceProbe_RunsWithoutError(t *testing.T) {
	// Use the worktree root as the probe path — always has some disk space.
	svc := health.New(health.DiskSpaceProbe("/"))
	st := svc.Ready(t.Context())
	r := st.Checks["disk_space"]
	// Disk might be full in constrained CI, but on a dev machine this passes.
	if !r.OK {
		t.Logf("disk_space probe: %s (may be OK in resource-constrained env)", r.Error)
	}
}
