# Profiling & Benchmarking

## Go Module Verification

The project uses Go's default module checksum database (`sum.golang.org`).
If you're behind a corporate proxy or in an air-gapped environment, set:

```bash
export GONOSUMCHECK=github.com/pavelveter/hermem
export GONOSUMDB=github.com/pavelveter/hermem
```

This skips checksum verification for the hermem module. Not recommended
for production builds — use only for local development in restricted networks.

## Running Benchmarks

```bash
# Run all benchmarks
make benchmarks

# Run specific benchmark
go test -bench=BenchmarkCosine -benchmem -count=3 ./src/internal/vector/...

# Compare against baseline
go install golang.org/x/perf/cmd/benchstat@latest
go test -bench=. -benchmem -count=6 -run='^$' ./src/... | tee bench_new.txt
benchstat bench/baseline/baseline.txt bench_new.txt
```

## Hot Paths

The following functions are performance-critical and should be benchmarked on every release:

| Function | Package | Why |
|----------|---------|-----|
| `BatchDotProducts` | `vector` | Core of all vector search operations |
| `CosineSimilarity` | `vector` | Used in dedup and retrieval scoring |
| `InMemoryVectorIndex.SearchBatch` | `vector` | Brute-force batch search, O(n·q) |
| `RetrievalPipeline.Execute` | `retrieval` | Full retrieval pipeline latency |
| `store.splitSQL` | `store` | Migration startup cost |

## Regression Gates

CI runs benchmarks on every PR touching `src/**/*.go`. The workflow:
1. Runs benchmarks with `-count=6` for statistical significance
2. Compares against `bench/baseline/baseline.txt` using `benchstat`
3. Fails if any hot-path benchmark regresses >5%

## Updating Baseline

After a release, update the baseline:

```bash
go test -bench=. -benchmem -count=6 -run='^$' ./src/... > bench/baseline/baseline.txt
git add bench/baseline/baseline.txt
git commit -m "chore: update benchmark baseline for v0.X.0"
```

## Known Performance Budgets

See `docs/perf-budgets.md` for accepted complexity characteristics.
