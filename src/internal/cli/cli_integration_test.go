package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

// testEnv creates an Env backed by a temporary SQLite file.
// cobra's PersistentPreRunE calls env.EnsureDB() which opens the DB,
// runs migrations, and builds the vector index from the temp path.
func testEnv(t *testing.T) *clienv.Env {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DBPath:    filepath.Join(dir, "hermem_test.db"),
		Schema:    core.DefaultSchemaConfig(false),
		VectorDim: 3,
		// §4 audit closure: tests legitimately want the apply-on-open
		// ergonomic so a freshly-created DB doesn't trip the
		// production refusal-mode gate. Production refuses to boot
		// against an out-of-date schema; tests opt-in to apply so the
		// migration runner is exercised end-to-end without per-test
		// `migrate apply` boilerplate.
		AutoMigrate: true,
	}
	env := &clienv.Env{
		// Ctx must be non-nil: env.DB.PingContext(env.Ctx) and every QueryContext
		// call dereference ctx internally; a nil context.Context interface triggers
		// a panic inside database/sql while holding db.mu, which then wedges
		// t.Cleanup(env.Close) on the same mutex and produces a deterministic 240s
		// timeout. See the fix(cli) commit notes for the deadlock trace.
		Ctx: t.Context(),
		Cfg: cfg,
		// Embedder must be wired because cli/memory/{store,search,edge},
		// cli/task/create, and friends call env.Embedder.Embed(env.Ctx, content)
		// inline. Tests bypass main.go (which sets env.Embedder from config) so
		// the field is otherwise zero-value nil → nil-method-receiver panic at
		// runtime. FakeEmbedder is deterministic + offline (no Ollama needed).
		Embedder: &fakeEmbedder{dim: cfg.VectorDim},
		// KeepDBOpen stops root.PersistentPostRunE from closing env.DB after
		// each executeCmd. Tests in this file routinely run TWO cmds per test
		// (e.g. TestCLI_MemoryEdge stores src + tgt then adds the edge); without
		// this flag, the second cmd runs against a closed DB and fails fast with
		// `sql: database is closed`. t.Cleanup(env.Close) below still handles
		// final teardown.
		KeepDBOpen: true,
	}
	t.Cleanup(func() { env.Close() })
	return env
}

func testStatefulEnv(t *testing.T) *clienv.Env {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DBPath:    filepath.Join(dir, "hermem_stateful_test.db"),
		Schema:    statefulSchema(),
		VectorDim: 3,
		// §4 audit closure: same opt-in as testEnv; explicitly opt-in
		// to apply-on-open so the apply path is exercised end-to-end in
		// every stateful test fixture.
		AutoMigrate: true,
	}
	env := &clienv.Env{
		// See testEnv; same Ctx + Embedder requirements apply.
		Ctx: t.Context(),
		Cfg: cfg,
		// See testEnv; by the same tests-multi-executeCmd logic,
		// KeepDBOpen is required here too. State tests like
		// TestCLI_TaskDep issue task/create twice in a row.
		Embedder:   &fakeEmbedder{dim: cfg.VectorDim},
		KeepDBOpen: true,
	}
	t.Cleanup(func() { env.Close() })
	return env
}

func statefulSchema() core.SchemaConfig {
	s := core.DefaultSchemaConfig(true)
	s.AllowedCategories["task"] = true
	s.StatefulCategories["task"] = true
	s.ValidStates = map[string]bool{"pending": true, "running": true, "completed": true}
	s.ValidStateOrder = []string{"pending", "running", "completed"}
	return s
}

// executeCmd runs a CLI command via cobra with piped stdin.
// Since cli.DecodeStdin reads os.Stdin directly (not cobra's InOrStdin),
// we must replace os.Stdin with a pipe for the command duration.
func executeCmd(t *testing.T, env *clienv.Env, args []string, stdinJSON interface{}) (string, error) {
	t.Helper()

	// Build stdin pipe for commands that read it
	var stdinData string
	if stdinJSON != nil {
		data, err := json.Marshal(stdinJSON)
		if err != nil {
			t.Fatalf("json marshal stdin: %v", err)
		}
		stdinData = string(data)
	}

	// Replace os.Stdin with a pipe
	oldStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = oldStdin })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r

	// Write stdin data and close
	go func() {
		if stdinData != "" {
			w.Write([]byte(stdinData))
		}
		w.Close()
	}()

	cmd := NewRootCommand(env)
	cmd.SetArgs(args)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.SetOut(&stdoutBuf)
	cmd.SetErr(&stderrBuf)

	execErr := cmd.Execute()
	return stdoutBuf.String() + stderrBuf.String(), execErr
}

// --- Version ---

func TestCLI_Version(t *testing.T) {
	env := &clienv.Env{Build: clienv.BuildInfo{Version: "1.0.0", BuildDate: "today", GitCommit: "abc123"}}
	out, err := executeCmd(t, env, []string{"version"}, nil)
	if err != nil {
		t.Fatalf("version: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "1.0.0") {
		t.Fatalf("want version in output: %s", out)
	}
}

// --- Health ---

func TestCLI_Health(t *testing.T) {
	env := testEnv(t)
	out, err := executeCmd(t, env, []string{"health"}, nil)
	if err != nil {
		t.Fatalf("health: %v\noutput: %s", err, out)
	}
	if out == "" {
		t.Fatal("expected non-empty health output")
	}
}

// --- DB commands ---

func TestCLI_DBVerify(t *testing.T) {
	env := testEnv(t)
	_, err := executeCmd(t, env, []string{"db", "verify"}, nil)
	if err != nil {
		t.Fatalf("db verify: %v", err)
	}
}

func TestCLI_DBMigrate(t *testing.T) {
	env := testEnv(t)
	_, err := executeCmd(t, env, []string{"db", "migrate"}, nil)
	if err != nil {
		t.Fatalf("db migrate: %v", err)
	}
}

func TestCLI_DBSchema(t *testing.T) {
	env := testEnv(t)
	_, err := executeCmd(t, env, []string{"db", "schema"}, nil)
	if err != nil {
		t.Fatalf("db schema: %v", err)
	}
}

// --- Graph commands ---

func TestCLI_GraphComponents(t *testing.T) {
	env := testEnv(t)
	_, err := executeCmd(t, env, []string{"graph", "components"}, nil)
	if err != nil {
		t.Fatalf("graph components: %v", err)
	}
}

func TestCLI_GraphCommunities(t *testing.T) {
	env := testEnv(t)
	_, err := executeCmd(t, env, []string{"graph", "communities"}, nil)
	if err != nil {
		t.Fatalf("graph communities: %v", err)
	}
}

func TestCLI_GraphVerify(t *testing.T) {
	env := testEnv(t)
	_, err := executeCmd(t, env, []string{"graph", "verify"}, nil)
	if err != nil {
		t.Fatalf("graph verify: %v", err)
	}
}

func TestCLI_GraphContradictions(t *testing.T) {
	env := testEnv(t)
	_, err := executeCmd(t, env, []string{"graph", "contradictions"}, nil)
	if err != nil {
		t.Fatalf("graph contradictions: %v", err)
	}
}

func TestCLI_GraphProvenance_RejectsNoFilter(t *testing.T) {
	env := testEnv(t)
	_, err := executeCmd(t, env, []string{"graph", "provenance"}, nil)
	if err == nil {
		t.Fatal("expected error for no provenance filter")
	}
}

// --- Memory commands ---

func TestCLI_MemoryStore(t *testing.T) {
	env := testEnv(t)
	req := map[string]string{"id": "store-test", "category": "world", "content": "stored via CLI"}
	out, err := executeCmd(t, env, []string{"memory", "store"}, req)
	if err != nil {
		t.Fatalf("memory store: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "ok") {
		t.Logf("memory store output: %s", out)
	}
}

func TestCLI_MemoryStore_RejectsNoCategory(t *testing.T) {
	env := testEnv(t)
	req := map[string]string{"id": "fail", "content": "test"}
	_, err := executeCmd(t, env, []string{"memory", "store"}, req)
	if err == nil {
		t.Fatal("expected error for missing category")
	}
}

func TestCLI_MemoryEdge(t *testing.T) {
	env := testEnv(t)
	// Store entities first
	_, err := executeCmd(t, env, []string{"memory", "store"}, map[string]string{"id": "src-e1", "category": "world", "content": "source"})
	if err != nil {
		t.Fatalf("store source: %v", err)
	}
	_, err = executeCmd(t, env, []string{"memory", "store"}, map[string]string{"id": "tgt-e1", "category": "world", "content": "target"})
	if err != nil {
		t.Fatalf("store target: %v", err)
	}

	req := map[string]interface{}{"source_id": "src-e1", "target_id": "tgt-e1", "relation_type": "related_to"}
	out, err := executeCmd(t, env, []string{"memory", "edge"}, req)
	if err != nil {
		t.Fatalf("memory edge: %v\noutput: %s", err, out)
	}
}

func TestCLI_MemoryEdge_RejectsNoSource(t *testing.T) {
	env := testEnv(t)
	req := map[string]string{"target_id": "t", "relation_type": "related_to"}
	_, err := executeCmd(t, env, []string{"memory", "edge"}, req)
	if err == nil {
		t.Fatal("expected error for missing source_id")
	}
}

// --- Task commands ---

func TestCLI_TaskList(t *testing.T) {
	env := testStatefulEnv(t)
	_, err := executeCmd(t, env, []string{"task", "list"}, map[string]string{})
	if err != nil {
		t.Fatalf("task list: %v", err)
	}
}

func TestCLI_TaskShow_RejectsNoID(t *testing.T) {
	env := testStatefulEnv(t)
	_, err := executeCmd(t, env, []string{"task", "show"}, map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestCLI_TaskCreate(t *testing.T) {
	env := testStatefulEnv(t)
	req := map[string]string{"id": "cli-task-1", "content": "created via CLI"}
	out, err := executeCmd(t, env, []string{"task", "create"}, req)
	if err != nil {
		t.Fatalf("task create: %v\noutput: %s", err, out)
	}
}

func TestCLI_TaskDep(t *testing.T) {
	env := testStatefulEnv(t)
	// Create two tasks
	_, err := executeCmd(t, env, []string{"task", "create"}, map[string]string{"id": "dep-a", "content": "dep a"})
	if err != nil {
		t.Fatalf("create dep-a: %v", err)
	}
	_, err = executeCmd(t, env, []string{"task", "create"}, map[string]string{"id": "dep-b", "content": "dep b"})
	if err != nil {
		t.Fatalf("create dep-b: %v", err)
	}

	req := map[string]interface{}{"source_id": "dep-a", "target_id": "dep-b", "add": true}
	out, err := executeCmd(t, env, []string{"task", "dep"}, req)
	if err != nil {
		t.Fatalf("task dep: %v\noutput: %s", err, out)
	}
}

func TestCLI_TaskTree(t *testing.T) {
	env := testStatefulEnv(t)
	_, err := executeCmd(t, env, []string{"task", "tree"}, map[string]string{})
	if err != nil {
		t.Fatalf("task tree: %v", err)
	}
}

func TestCLI_TaskExecutable(t *testing.T) {
	env := testStatefulEnv(t)
	_, err := executeCmd(t, env, []string{"task", "executable"}, nil)
	if err != nil {
		t.Fatalf("task executable: %v", err)
	}
}

// --- Time commands ---

func TestCLI_TimeTimeline(t *testing.T) {
	env := testEnv(t)
	out, err := executeCmd(t, env, []string{"time", "timeline"}, nil)
	if err != nil {
		t.Fatalf("time timeline: %v\noutput: %s", err, out)
	}
	if out == "" {
		t.Log("timeline output empty (expected on fresh DB)")
	}
}

// --- Graph plan / recovery-plan ---

func TestCLI_GraphRecoveryPlan_RejectsNoID(t *testing.T) {
	env := testStatefulEnv(t)
	_, err := executeCmd(t, env, []string{"graph", "recovery-plan"}, map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

// --- Memory search ---

func TestCLI_MemorySearch(t *testing.T) {
	env := testEnv(t)
	// Search requires stored entities with embeddings
	req := map[string]interface{}{"query": "test", "top_k": 3}
	out, err := executeCmd(t, env, []string{"memory", "search"}, req)
	if err != nil {
		t.Fatalf("memory search: %v\noutput: %s", err, out)
	}
}

// --- Fake embedder (test-only) ---
//
// fakeEmbedder hashes content with FNV-1a into a dim-dim unit vector. Tests
// run fully offline (no Ollama / OpenAI HTTP) and get deterministic vectors,
// so a "hello" entity and a "world" entity always produce distinct unit
// vectors; the same content always produces the same vector across runs.
//
// The hash is folded across dims by reading 8 bits at a time out of the
// 32-bit FNV seed; for tests with VectorDim=3 the first three bytes cover
// the vector. For larger dims the bits fold deterministically; collisions
// are acceptable for test fixtures, not for production embedding.
type fakeEmbedder struct {
	dim int
}

func (f *fakeEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	if f.dim <= 0 {
		return []float32{}, nil
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(content))
	seed := h.Sum32()

	v := make([]float32, f.dim)
	for i := 0; i < f.dim; i++ {
		v[i] = float32((seed>>uint((i%4)*8))&0xff) / 255.0
	}

	// Normalise to unit length so cosine-similarity comparisons across test
	// embeddings behave like production. Zero-vector fallback returns the
	// standard basis vector e0 — keeps vector-index.Store happy on empty input.
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	if sumSq == 0 {
		v[0] = 1.0
		return v, nil
	}
	inv := 1.0 / math.Sqrt(sumSq)
	for i := range v {
		v[i] = float32(float64(v[i]) * inv)
	}
	return v, nil
}

func (f *fakeEmbedder) Ping(_ context.Context) error {
	return nil
}

// Compile-time check that fakeEmbedder satisfies core.Embedder.
var _ core.Embedder = (*fakeEmbedder)(nil)
