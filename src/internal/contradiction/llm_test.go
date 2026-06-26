package contradiction

import (
	"context"
	"errors"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// mockLLMChecker is a test double for LLMChecker.
type mockLLMChecker struct {
	contradicts bool
	confidence  float32
	err         error
	calls       int
}

func (m *mockLLMChecker) IsContradiction(_ context.Context, _, _ string) (bool, float32, error) {
	m.calls++
	return m.contradicts, m.confidence, m.err
}

func TestLLMDetector(t *testing.T) {
	cases := []struct {
		name      string
		existing  core.Entity
		incoming  core.Entity
		checker   *mockLLMChecker
		want      bool
		wantCalls int
	}{
		{
			name:      "contradiction_detected",
			existing:  core.Entity{Content: "Go is fast"},
			incoming:  core.Entity{Content: "Go is slow"},
			checker:   &mockLLMChecker{contradicts: true, confidence: 0.9},
			want:      true,
			wantCalls: 1,
		},
		{
			name:      "no_contradiction",
			existing:  core.Entity{Content: "Go is fast"},
			incoming:  core.Entity{Content: "Go is performant"},
			checker:   &mockLLMChecker{contradicts: false, confidence: 0.1},
			want:      false,
			wantCalls: 1,
		},
		{
			name:      "llm_error_returns_miss",
			existing:  core.Entity{Content: "a"},
			incoming:  core.Entity{Content: "b"},
			checker:   &mockLLMChecker{err: errors.New("llm timeout")},
			want:      false,
			wantCalls: 1,
		},
		{
		 name:      "nil_checker_returns_miss",
			existing:  core.Entity{Content: "a"},
			incoming:  core.Entity{Content: "b"},
			checker:   nil,
			want:      false,
			wantCalls: 0,
		},
		{
			name:      "zero_confidence_clamped_to_half",
			existing:  core.Entity{Content: "a"},
			incoming:  core.Entity{Content: "b"},
			checker:   &mockLLMChecker{contradicts: true, confidence: 0},
			want:      true,
			wantCalls: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var d *LLMDetector
			if c.checker == nil {
				d = NewLLMDetector(nil)
			} else {
				d = NewLLMDetector(c.checker)
			}
			result := d.Detect(c.existing, c.incoming)
			if result.Detected != c.want {
				t.Errorf("Detect() detected=%v, want %v", result.Detected, c.want)
			}
			if c.checker != nil && c.checker.calls != c.wantCalls {
				t.Errorf("checker called %d times, want %d", c.checker.calls, c.wantCalls)
			}
			if result.Detected && result.Reason == "" {
				t.Error("hit but reason empty")
			}
			if !result.Detected && result.Reason != "" {
				t.Errorf("miss but reason non-empty (%q)", result.Reason)
			}
		})
	}
}
