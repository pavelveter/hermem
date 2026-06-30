//go:build darwin && cgo

package vector

import (
	"testing"
	"time"
)

// §11 AMX / Accelerate runtime guard.
//
// Build selection: this file is selected ONLY on darwin with cgo enabled.
// On non-darwin CI (linux/windows) or darwin-without-cgo, the test is
// simply absent — the CGo preamble in cosine_darwin.go is not compiled,
// so BatchDotProducts resolves to the pure-Go fallback in cosine.go
// (which has its own coverage in cosine_test.go).
//
// Why a wall-time assertion instead of a benchmark-floor check: a
// `Benchmark` is informational and never fails `go test`. For a CI
// guard that must catch silent Accelerate degradation (linker drops
// cblas, build-tag drift, cgo disabled at link time), the test must
// fail the build when the path is unexpectedly slow. The Test form
// below does that via a 2ms per-call ceiling — see the threshold
// rationale inline. See ADR-021.

// amxHotRows / amxHotCols match the §10 retrieval hot path: 1K
// candidate facts × 768-dim embeddings. 1024 × 768 is the realistic
// shape where AMX acceleration actually helps — smaller shapes fit
// in L1 cache and the SIMD speedup is less pronounced; larger shapes
// are dominated by memory bandwidth, not compute.
const (
	amxHotRows = 1024
	amxHotCols = 768
)

// amxPerCallThreshold is the wall-time ceiling for one 1024×768
// batched-dot call on the AMX-accelerated path. The selection is
// driven by the 5-15× AMX-vs-pure-Go speedup documented in the
// cosine_darwin.go preamble:
//
//	AMX on M-series silicon:    0.2 - 0.5 ms
//	Pure-Go on M-series silicon: 5 - 15 ms
//	AMX-degraded (= pure-Go):     same 5 - 15 ms
//
// 2 ms sits well above the AMX-fast range (4-10× headroom for CI
// load variance) and well below the pure-Go range (2.5-7.5× safety
// margin against false-positive failure). If the test fails on
// GitHub Actions macos-latest, the most likely causes are (a) the
// macos runner hardware was downgraded (Intel fallback) or (b) a
// future refactor accidentally disabled the cgo build. Both warrant
// investigation rather than threshold relaxation.
const amxPerCallThreshold = 2 * time.Millisecond

// makeAMXWorkload builds the canonical 1024×768 query + matrix +
// output triple used by both the test and the benchmark. Extracted
// so the two share one source of truth for the input shape — a
// drift between the Test and the Benchmark would be silently
// self-cancelling.
func makeAMXWorkload() (q, m, dots []float32) {
	q = make([]float32, amxHotCols)
	for i := range q {
		q[i] = float32(i+1) * 0.001
	}
	m = make([]float32, amxHotRows*amxHotCols)
	for r := 0; r < amxHotRows; r++ {
		for c := 0; c < amxHotCols; c++ {
			m[r*amxHotCols+c] = float32(r+c+1) * 0.0001
		}
	}
	dots = make([]float32, amxHotRows)
	return q, m, dots
}

// TestBatchDot_AMXGuard is the §11 runtime guard. It runs the
// 1024×768 batched dot 100 times and asserts the average wall time
// per call stays below amxPerCallThreshold. This is the assertion
// that catches silent Accelerate degradation: if cblas_sgemv is
// not actually being invoked (linker dropped Accelerate, build-tag
// drift, cgo accidentally disabled), the path falls through to the
// pure-Go loop in cosine.go and the per-call time jumps from
// ~0.3ms to ~10ms — well above the 2ms ceiling.
//
// The test logs the actual measured per-call time on success so a
// CI dashboard can chart the trend; the failure message includes
// the threshold and the measured value so an investigator doesn't
// have to re-run locally to see what happened.
func TestBatchDot_AMXGuard(t *testing.T) {
	q, m, dots := makeAMXWorkload()

	// Warm up: 3 calls to flush first-touch page faults and let the
	// branch predictor settle. These calls are NOT counted toward
	// the measurement window; without the warmup, the first call
	// is ~5-10× slower than steady-state and a single measurement
	// can be misleadingly pessimistic.
	for i := 0; i < 3; i++ {
		BatchDotProducts(q, m, amxHotRows, amxHotCols, dots)
	}

	// Measure: 100 iterations divided by total elapsed = per-call.
	// 100 is the smallest round number that brings the total time
	// well above the timer resolution (each call is ~0.3ms, so 100
	// calls = ~30ms — 30× above the ~1ms timer granularity).
	const iterations = 100
	start := time.Now()
	for i := 0; i < iterations; i++ {
		BatchDotProducts(q, m, amxHotRows, amxHotCols, dots)
	}
	elapsed := time.Since(start)
	perCall := elapsed / iterations

	if perCall > amxPerCallThreshold {
		t.Fatalf("AMX guard FAIL: BatchDotProducts(%dx%d) = %v per call > %v threshold — Accelerate is likely silently degraded. Common causes: (1) linker dropped -framework Accelerate, (2) build tag drift on cosine_darwin.go, (3) cgo accidentally disabled at link time. Verify with: nm ./hermem | grep cblas_sgemv && CGO_ENABLED=1 go test -count=1 -v -run TestBatchDot_AMXGuard ./src/internal/vector/...",
			amxHotRows, amxHotCols, perCall, amxPerCallThreshold)
	}
	t.Logf("AMX guard OK: BatchDotProducts(%dx%d) = %v per call (threshold %v)",
		amxHotRows, amxHotCols, perCall, amxPerCallThreshold)
}

// BenchmarkBatchDot_AMX tracks the AMX-accelerated batched-dot
// throughput over time. The b.N-driven outer loop calibrates the
// inner workload to the benchtime flag (default 1s); the
// ReportMetric line emits a `dot/sec` custom metric that a CI
// trend dashboard can scrape.
//
// This benchmark is informational — it does NOT fail CI on slow
// runs (a `Benchmark` never fails `go test`). The companion
// TestBatchDot_AMXGuard above is the gatekeeper; this benchmark
// is for performance regression tracking and for verifying the
// 5-15× speedup claim on real hardware.
func BenchmarkBatchDot_AMX(b *testing.B) {
	q, m, dots := makeAMXWorkload()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		BatchDotProducts(q, m, amxHotRows, amxHotCols, dots)
	}
	elapsed := time.Since(start)
	// 1024 × 768 = 786,432 dot products per call.
	dotProductsPerCall := float64(amxHotRows * amxHotCols)
	b.ReportMetric(float64(b.N)*dotProductsPerCall/elapsed.Seconds(), "dot/sec")
}
