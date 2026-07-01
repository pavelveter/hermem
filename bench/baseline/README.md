# Benchmark Baseline

This directory holds the baseline benchmark results for regression detection.

## Generating a baseline

Run from the project root:

```bash
go test -bench=. -benchmem -count=6 -timeout 300s \
  -run='^$' ./src/... | tee bench/baseline/baseline.txt
```

The `bench.yml` CI workflow compares new results against this file on
every PR that touches `src/**/*.go`. If the baseline is missing, the
workflow falls back to standalone `benchstat` analysis (no regression gate).
