#!/bin/bash
# Hermem benchmark suite runner.
# Usage: ./bench.sh [filter] [count]
#   filter - optional benchmark name filter (passed to -bench)
#   count  - how many times to run (default 3)
#
# Examples:
#   ./bench.sh                    # run all benchmarks with default count
#   ./bench.sh InMemory            # run only InMemory benchmarks
#   ./bench.sh "" 5                # run all 5 times

set -euo pipefail

FILTER="${1:-.}"
COUNT="${2:-3}"
DIR="$(cd "$(dirname "$0")" && pwd)"
OUTDIR="${DIR}/bench_results"
mkdir -p "$OUTDIR"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTFILE="${OUTDIR}/bench_${TIMESTAMP}.txt"

echo "=== Hermem Benchmark Suite ==="
echo "Filter:  $FILTER"
echo "Count:   $COUNT"
echo "Output:  $OUTFILE"
echo

cd "$DIR"

# Run benchmarks and save raw output
go test -bench="$FILTER" -benchmem -count="$COUNT" -timeout 600s \
    ./src/... 2>&1 | tee "$OUTFILE"

# Print summary
echo
echo "=== Summary ==="
grep -E '^Benchmark|^ok |^FAIL' "$OUTFILE" | head -30
echo
echo "Full results: $OUTFILE"
