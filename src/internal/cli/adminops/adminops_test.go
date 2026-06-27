package adminops

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"

	_ "github.com/mattn/go-sqlite3"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func testAdminEnv(t *testing.T) *cli.Env {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DBPath:    dir + "/hermem_test.db",
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
	env := &cli.Env{
		Ctx:        t.Context(),
		Cfg:        cfg,
		KeepDBOpen: true,
	}
	if err := env.EnsureDB(); err != nil {
		t.Fatalf("EnsureDB: %v", err)
	}
	t.Cleanup(func() { env.Close() })
	return env
}

func seedOpsTestDB(t *testing.T, env *cli.Env) {
	t.Helper()
	db := env.DB
	schema := `
	CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY, category TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '', embedding BLOB,
		archived INTEGER DEFAULT 0, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS edges (
		source_id TEXT NOT NULL, target_id TEXT NOT NULL,
		relation_type TEXT NOT NULL,
		PRIMARY KEY (source_id, target_id, relation_type)
	);`
	db.Exec(schema)
	for i := 0; i < 10; i++ {
		id := "e" + itoa(i)
		emb := make([]byte, 16)
		db.Exec("INSERT OR IGNORE INTO entities (id, category, content, embedding) VALUES (?, 'test', 'content', ?)", id, emb)
	}
	for i := 0; i < 5; i++ {
		src := "e" + itoa(i)
		tgt := "e" + itoa((i+1)%10)
		db.Exec("INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES (?, ?, 'related_to')", src, tgt)
	}
}

func executeCmd(cmd *cobra.Command, args []string) (string, error) {
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestOpsStats(t *testing.T) {
	env := testAdminEnv(t)
	seedOpsTestDB(t, env)
	cmd := newStatsCmd(env)
	out, err := executeCmd(cmd, []string{})
	if err != nil {
		t.Fatalf("ops stats: %v", err)
	}
	if !strings.Contains(out, "Node count") {
		t.Errorf("output missing 'Node count': %s", out)
	}
	if !strings.Contains(out, "Edge count") {
		t.Errorf("output missing 'Edge count': %s", out)
	}
}

func TestOpsStatsJSON(t *testing.T) {
	env := testAdminEnv(t)
	seedOpsTestDB(t, env)
	cmd := newStatsCmd(env)
	out, err := executeCmd(cmd, []string{"--json"})
	if err != nil {
		t.Fatalf("ops stats --json: %v", err)
	}
	if !strings.Contains(out, "node_count") {
		t.Errorf("JSON output missing node_count: %s", out)
	}
}

func TestOpsIntegrity_OK(t *testing.T) {
	env := testAdminEnv(t)
	seedOpsTestDB(t, env)
	cmd := newIntegrityCmd(env)
	out, err := executeCmd(cmd, []string{})
	if err != nil {
		// Exit 1 from os.Exit(1) triggers Execute error
		if !strings.Contains(out, "CRIT") {
			t.Fatalf("unexpected error: %v, output: %s", err, out)
		}
	}
	if !strings.Contains(out, "OK") && !strings.Contains(out, "no issues") {
		t.Errorf("expected OK output, got: %s", out)
	}
}

func TestOpsIntegrity_WithMissingEmbedding(t *testing.T) {
	env := testAdminEnv(t)
	seedOpsTestDB(t, env)
	// Add entity without embedding
	env.DB.Exec("INSERT INTO entities (id, category, content) VALUES ('missing-emb', 'test', 'x')") //nolint:errcheck // test bootstrap: subsequent Query surfaces failure

	cmd := newIntegrityCmd(env)
	out, err := executeCmd(cmd, []string{"--json"})
	if err != nil {
		// os.Exit(1) causes Execute to return error
		if !strings.Contains(out, "MISSING_EMBEDDING") {
			t.Fatalf("unexpected error: %v, output: %s", err, out)
		}
	}
	if !strings.Contains(out, "MISSING_EMBEDDING") {
		t.Errorf("expected MISSING_EMBEDDING in output: %s", out)
	}
}

func TestOpsVacuum(t *testing.T) {
	env := testAdminEnv(t)
	seedOpsTestDB(t, env)
	cmd := newVacuumCmd(env)
	out, err := executeCmd(cmd, []string{"--no-progress"})
	if err != nil {
		t.Fatalf("ops vacuum: %v", err)
	}
	if !strings.Contains(out, "VACUUM complete") {
		t.Errorf("expected 'VACUUM complete', got: %s", out)
	}
}

func TestOpsRebuildIndex_DryRun(t *testing.T) {
	env := testAdminEnv(t)
	seedOpsTestDB(t, env)
	// Ensure Embedder is set (needed by rebuild-index command type-assertion)
	env.Embedder = &fakeEmbedder{dim: 3}
	// VI is set by EnsureDB via vector.NewIndex

	cmd := newRebuildIndexCmd(env)
	out, err := executeCmd(cmd, []string{"--dry-run"})
	if err != nil {
		t.Fatalf("ops rebuild-index --dry-run: %v", err)
	}
	if !strings.Contains(out, "Would re-embed") {
		t.Errorf("expected 'Would re-embed', got: %s", out)
	}
}

type fakeEmbedder struct {
	dim int
}

func (f *fakeEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	return make([]float32, f.dim), nil
}

func (f *fakeEmbedder) Ping(_ context.Context) error {
	return nil
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return ""
}
