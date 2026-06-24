package env

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
)

// validCfg returns a minimally-valid Config that passes the new
// cfg.Validate() requirements (provider non-empty + dim > 0 + timeouts
// > 0 + URL with scheme+host). Tests that need an invalid cfg should
// construct their own zero-values or strip fields explicitly.
func validCfg() *config.Config {
	return &config.Config{
		Provider:          "ollama",
		URL:               "http://localhost:11434",
		VectorDim:         768,
		EmbedderTimeout:   30 * time.Second,
		ExtractTimeout:    60 * time.Second,
		DedupThreshold:    0.88,
		MaxDepthCeiling:   5,
		MaxRetrievedNodes: 100,
		VectorBackend:     "in-memory",
		Model:             "nomic-embed-text",
		ExtractModel:      "qwen2.5-coder:7b",
		ExtractProvider:   "ollama",
		ExtractURL:        "http://localhost:11434",
		Retention:         core.RetentionPolicy{},
		Schema:            core.DefaultSchemaConfig(false),
	}
}

// TestEnvManager_GetReturnsInitial — construction with initial env yields
// that exact pointer to Get().
func TestEnvManager_GetReturnsInitial(t *testing.T) {
	env := &Env{Ctx: context.Background()}
	m := NewEnvManager(env)
	if got := m.Get(); got != env {
		t.Fatalf("Get: want %p, got %p", env, got)
	}
}

// TestEnvManager_GetNilSafe — NewEnvManager(nil) must NOT panic and Get()
// returns nil so callers can branch.
func TestEnvManager_GetNilSafe(t *testing.T) {
	m := NewEnvManager(nil)
	if got := m.Get(); got != nil {
		t.Fatalf("Get: want nil, got %p", got)
	}
}

// TestEnvManager_SetSwapsAtomically — Set replaces the visible snapshot
// for every subsequent Get call.
func TestEnvManager_SetSwapsAtomically(t *testing.T) {
	m := NewEnvManager(&Env{Ctx: context.Background(), Cfg: &config.Config{}})
	env2 := &Env{Ctx: context.Background(), Cfg: &config.Config{VectorDim: 99}}
	m.Set(env2)
	if got := m.Get(); got != env2 || got.Cfg.VectorDim != 99 {
		t.Fatalf("Get after Set: want pointer to env2 (dim=99), got %+v", got)
	}
}

// TestEnvManager_ReloadValidatesAndSwaps — valid cfg passes; the returned
// *Env is the one stashed behind atomic.Pointer.
func TestEnvManager_ReloadValidatesAndSwaps(t *testing.T) {
	m := NewEnvManager(&Env{
		Ctx:   context.Background(),
		Cfg:   validCfg(),
		Build: BuildInfo{Version: "v1"},
	})
	cfg := validCfg()
	newEnv, err := m.Reload(cfg)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := m.Get(); got != newEnv {
		t.Fatalf("Get after Reload: want pointer to newEnv, got %p vs %p", got, newEnv)
	}
	if newEnv.Build.Version != "v1" {
		t.Fatalf("Build.Version: want preserved v1, got %q", newEnv.Build.Version)
	}
}

// TestEnvManager_ReloadRejectsInvalid — bad cfg is rejected without
// mutating the existing Env.
func TestEnvManager_ReloadRejectsInvalid(t *testing.T) {
	original := &Env{Ctx: context.Background()}
	m := NewEnvManager(original)
	if _, err := m.Reload(&config.Config{}); err == nil {
		t.Fatal("expected validate error, got nil")
	}
	if got := m.Get(); got != original {
		t.Fatalf("Get after invalid Reload: want original pointer preserved, got %p", got)
	}
}

// TestEnvManager_ConcurrentGetSet — race-detector smoke. Spawns readers
// and writers; under -race we want zero "DATA RACE" reports. Goroutines
// report failures via a `broken` atomic.Bool rather than t.Fatal so
// go-vet's copylocks + non-test-goroutine Fatal checks stay clean.
func TestEnvManager_ConcurrentGetSet(t *testing.T) {
	const goroutines = 32
	var broken int32 // 0 = healthy, 1 = invariant violated
	m := NewEnvManager(&Env{Ctx: context.Background()})
	seen := sync.Map{}

	var wg sync.WaitGroup
	wg.Add(2 * goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			env := &Env{Ctx: context.Background()}
			m.Set(env)
			seen.Store(env, struct{}{})
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if env := m.Get(); env == nil {
					atomic.StoreInt32(&broken, 1)
					return
				}
			}
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&broken) == 1 {
		t.Fatal("Get returned nil during concurrent Set — atomic.Pointer invariant broken")
	}
	count := 0
	seen.Range(func(_, _ any) bool { count++; return true })
	if count < goroutines {
		t.Fatalf("seen count: want %d, got %d", goroutines, count)
	}
}
