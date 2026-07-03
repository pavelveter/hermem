package vector

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newSeededIndex constructs an InMemoryVectorIndex directly (no DB, no load())
// with 3 pre-normalized 3-dim entries. Tests that need a working Search use this
// fixture. Per-entry `vec` is unit-length so Search divides dots by queryNorm
// and produces real cosine scores (consistent with what `load()` would store).
func newSeededIndex() *InMemoryVectorIndex {
	now := time.Now()
	entries := []vectorEntry{
		{id: "x", vec: []float32{1, 0, 0}, norm: 1, lastAccess: now},
		{id: "y", vec: []float32{0, 1, 0}, norm: 1, lastAccess: now},
		// z is (1/sqrt(2), 0, 1/sqrt(2)) — unit-norm diagonal in x/z plane.
		{id: "z", vec: []float32{0.7071068, 0, 0.7071068}, norm: 1, lastAccess: now},
	}
	flat := make([]float32, 0, 9)
	for _, e := range entries {
		flat = append(flat, e.vec...)
	}
	return &InMemoryVectorIndex{
		cols:       3,
		entries:    entries,
		flatMatrix: flat,
		byID:       map[string]int{"x": 0, "y": 1, "z": 2},
	}
}

// TestSearch_LimitOutOfRange verifies that Search rejects `limit` values
// outside [0, maxSearchLimit] with a wrapped ErrLimitOutOfRange BEFORE any
// result slice is allocated. Closes CodeQL alert #1
// (`go/uncontrolled-allocation-size`) for inmemory.go:202.
//
// Coverage:
//   - limit < 0          — was a panic via `make([]string, -1)`; now an error.
//   - limit == maxSearchLimit when n < maxSearchLimit — silently clamped to n.
//   - limit > maxSearchLimit — ErrLimitOutOfRange returned.
//   - in-bounds positive limits — return ranked results, no regression.
func TestSearch_LimitOutOfRange(t *testing.T) {
	t.Parallel()
	idx := newSeededIndex()

	cases := []struct {
		name        string
		limit       int
		wantErr     bool
		wantResults int
	}{
		{"negative_returns_err", -1, true, 0},
		{"zero_returns_empty_results", 0, false, 0},
		{"one_returns_one_id", 1, false, 1},
		{"three_returns_all_three_ids", 3, false, 3},
		// maxSearchLimit_passes_validation: lower bound of the in-bounds range.
		// The clamp `if limit > n { limit = n }` is incidental (n=3 < 500), and
		// what this subtest really verifies is that limit == maxSearchLimit is
		// accepted as the boundary. If a future maintainer grows the fixture
		// past maxSearchLimit, this case trivialises into a no-op; rename to
		// `maxSearchLimit_set_boundary` if you want a sharper boundary test.
		{"maxSearchLimit_passes_validation", maxSearchLimit, false, 3},
		{"maxSearchLimit_plus_one_returns_err", maxSearchLimit + 1, true, 0},
		{"five_hundred_one_returns_err", 501, true, 0},
	}

	for _, tc := range cases {
		tc := tc // capture range-var for t.Parallel safety
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := idx.Search(context.Background(), []float32{1, 1, 0}, tc.limit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("limit=%d: expected ErrLimitOutOfRange, got nil (results=%v)", tc.limit, out)
				}
				if !errors.Is(err, ErrLimitOutOfRange) {
					t.Fatalf("limit=%d: want errors.Is(ErrLimitOutOfRange), got %v", tc.limit, err)
				}
				if out != nil {
					t.Fatalf("limit=%d: want nil results on error, got %v", tc.limit, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("limit=%d: unexpected error: %v", tc.limit, err)
			}
			if len(out) != tc.wantResults {
				t.Fatalf("limit=%d: want %d results, got %d (%v)", tc.limit, tc.wantResults, len(out), out)
			}
		})
	}
}

// TestSearchBatch_LimitOutOfRange mirrors the Search case for SearchBatch.
// The original CodeQL alert was inside the per-query loop on
// `ids := make([]string, l)`; invalid limit must be rejected BEFORE entering
// the loop so the inner allocation is provably bounded by maxSearchLimit.
func TestSearchBatch_LimitOutOfRange(t *testing.T) {
	t.Parallel()
	idx := newSeededIndex()

	queries := [][]float32{
		{1, 1, 0},   // closer to {1,0,0} than {0,1,0}
		{0, 1, 0.5}, // closer to {0,1,0}
	}

	cases := []struct {
		name       string
		limit      int
		wantErr    bool
		wantRows   int // len(out)
		wantRowLen int // len(out[i]) for each i
	}{
		{"negative_returns_err", -1, true, 0, 0},
		{"zero_returns_empty_rows", 0, false, 2, 0},
		{"one_returns_one_id_per_query", 1, false, 2, 1},
		{"three_returns_all_three_ids_per_query", 3, false, 2, 3},
		// See boundary-case semantics explainer under TestSearch_LimitOutOfRange:
		// this confirms limit == maxSearchLimit is accepted by validation; the
		// per-query clamp to n is incidental.
		{"maxSearchLimit_passes_validation", maxSearchLimit, false, 2, 3},
		{"maxSearchLimit_plus_one_returns_err", maxSearchLimit + 1, true, 0, 0},
		{"five_hundred_one_returns_err", 501, true, 0, 0},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := idx.SearchBatch(context.Background(), queries, tc.limit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("limit=%d: expected ErrLimitOutOfRange, got nil (out=%v)", tc.limit, out)
				}
				if !errors.Is(err, ErrLimitOutOfRange) {
					t.Fatalf("limit=%d: want errors.Is(ErrLimitOutOfRange), got %v", tc.limit, err)
				}
				if out != nil {
					t.Fatalf("limit=%d: want nil out on error, got %v", tc.limit, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("limit=%d: unexpected error: %v", tc.limit, err)
			}
			if len(out) != tc.wantRows {
				t.Fatalf("limit=%d: want %d outer rows, got %d", tc.limit, tc.wantRows, len(out))
			}
			for qi, row := range out {
				if len(row) != tc.wantRowLen {
					t.Fatalf("limit=%d, query %d: want row len %d, got %d (%v)",
						tc.limit, qi, tc.wantRowLen, len(row), row)
				}
			}
		})
	}
}
