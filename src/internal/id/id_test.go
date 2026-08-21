package id

import (
	"fmt"
	"sync"
	"testing"
)

func TestNewTaskIDUniqueWithinProcess(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := NewTaskID()
		if seen[id] {
			t.Fatalf("duplicate task id %q", id)
		}
		seen[id] = true
	}
}

func TestNewTaskIDConcurrent(t *testing.T) {
	const goroutines = 32
	const per = 100
	seen := sync.Map{}
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				id := NewTaskID()
				if _, dup := seen.LoadOrStore(id, struct{}{}); dup {
					t.Errorf("duplicate task id under concurrency: %q", id)
				}
			}
		}()
	}
	wg.Wait()
}

func TestNewTaskIDFormat(t *testing.T) {
	// Format is the historical process-local counter: task-<n>. The
	// ADR-035 upgrade path may change this; this test pins the current
	// compatibility behavior so the facade removal doesn't silently
	// alter identity shape.
	first := NewTaskID()
	if len(first) < len("task-1") || first[:5] != "task-" {
		t.Fatalf("unexpected task id format: %q", first)
	}
	if _, err := fmt.Sscanf(first, "task-%d", new(uint64)); err != nil {
		t.Fatalf("task id %q is not task-<uint>: %v", first, err)
	}
}
