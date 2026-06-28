package config

import (
	"sync"
	"testing"
)

func TestAtomicConfig_LoadReturnsInitial(t *testing.T) {
	cfg := &Config{Provider: "ollama", DBPath: "test.db"}
	ac := NewAtomicConfig(cfg)
	got := ac.Load()
	if got != cfg {
		t.Fatal("Load should return the initial config")
	}
	if got.Provider != "ollama" {
		t.Fatalf("want provider 'ollama', got %q", got.Provider)
	}
}

func TestAtomicConfig_StoreReplacesConfig(t *testing.T) {
	cfg1 := &Config{Provider: "ollama"}
	cfg2 := &Config{Provider: "openai"}
	ac := NewAtomicConfig(cfg1)
	ac.Store(cfg2)
	got := ac.Load()
	if got != cfg2 {
		t.Fatal("Store should replace the config")
	}
	if got.Provider != "openai" {
		t.Fatalf("want provider 'openai', got %q", got.Provider)
	}
}

func TestAtomicConfig_SwapReturnsOld(t *testing.T) {
	cfg1 := &Config{Provider: "ollama"}
	cfg2 := &Config{Provider: "openai"}
	ac := NewAtomicConfig(cfg1)
	old := ac.Swap(cfg2)
	if old != cfg1 {
		t.Fatal("Swap should return the old config")
	}
	if ac.Load() != cfg2 {
		t.Fatal("Load after Swap should return new config")
	}
}

func TestAtomicConfig_ConcurrentReadsAndWrites(t *testing.T) {
	cfg := &Config{Provider: "ollama"}
	ac := NewAtomicConfig(cfg)
	var wg sync.WaitGroup
	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ac.Store(&Config{Provider: "provider_" + string(rune('a'+i))})
		}(i)
	}
	// Readers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := ac.Load()
			if got == nil {
				t.Error("Load returned nil")
			}
		}()
	}
	wg.Wait()
}
