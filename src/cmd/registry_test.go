package cmd

import (
	"reflect"
	"strings"
	"testing"
)

// Reset the registry between tests so test order doesn't matter.
func resetRegistry(t *testing.T) {
	t.Helper()
	registry = map[string]Handler{}
	// Re-run init() of every cmd file in-process isn't trivial in Go without
	// runtime tricks; tests rely on test-local Register() calls and don't
	// assert the full prod-name set.
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	resetRegistry(t)
	Register("dup", func(_ Env) {})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate Register")
		}
	}()
	Register("dup", func(_ Env) {})
}

func TestRun_UnknownNameReturnsFalse(t *testing.T) {
	resetRegistry(t)
	if Run("totally-unknown", Env{}) {
		t.Fatal("Run on unknown name must return false")
	}
}

func TestRun_DispatchesRegisteredHandler(t *testing.T) {
	resetRegistry(t)
	called := false
	Register("ping", func(_ Env) { called = true })
	if !Run("ping", Env{}) {
		t.Fatal("Run on registered name must return true")
	}
	if !called {
		t.Fatal("handler was not invoked")
	}
}

func TestRun_AliasRoutesSameHandler(t *testing.T) {
	resetRegistry(t)
	calls := 0
	shared := func(_ Env) { calls++ }
	Register("primary", shared)
	Register("alias", shared) // SAME handler under two names → true alias behaviour
	if !Run("primary", Env{}) {
		t.Fatal("Run on primary must return true")
	}
	if calls != 1 {
		t.Fatalf("after Run(primary): want 1, got %d", calls)
	}
	if !Run("alias", Env{}) {
		t.Fatal("Run on alias must return true")
	}
	if calls != 2 {
		t.Fatalf("after Run(alias): want 2, got %d", calls)
	}
}

func TestNames_Sorted(t *testing.T) {
	resetRegistry(t)
	Register("zeta", func(_ Env) {})
	Register("alpha", func(_ Env) {})
	Register("mu", func(_ Env) {})
	want := []string{"alpha", "mu", "zeta"}
	got := Names()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted names: want %v, got %v", want, got)
	}
}

func TestRegister_HelpfulPanicMessage(t *testing.T) {
	resetRegistry(t)
	Register("helper", func(_ Env) {})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T", r)
		}
		if !strings.Contains(msg, "helper") {
			t.Fatalf("panic message must mention duplicate name: %q", msg)
		}
	}()
	Register("helper", func(_ Env) {})
}
