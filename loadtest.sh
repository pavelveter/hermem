#!/bin/bash
# Hermem load testing runner.
# Requires: curl (for simple load), ab (Apache Bench, optional for heavier load).
# Usage: ./loadtest.sh [host] [port] [requests] [concurrency]
#   host       - default localhost
#   port       - default 8420
#   requests   - default 100
#   concurrency - default 10

set -euo pipefail

HOST="${1:-localhost}"
PORT="${2:-8420}"
REQUESTS="${3:-100}"
CONCURRENCY="${4:-10}"
BASE="http://${HOST}:${PORT}"
PASS=0
FAIL=0

cleanup() { echo; echo "Load test complete: $PASS passed, $FAIL failed."; }
trap cleanup EXIT

run_curl() {
    local method="$1" url="$2" data="$3" desc="$4"
    local code
    code=$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "$url" \
        -H 'Content-Type: application/json' -d "$data" 2>/dev/null || echo "000")
    if [ "$code" = "200" ] || [ "$code" = "204" ]; then
        ((PASS++)) || true
    else
        ((FAIL++)) || true
        [ "$FAIL" -le 5 ] && echo "  FAIL $desc: HTTP $code" >&2
    fi
}

echo "=== Hermem Load Test ==="
echo "Target: $BASE"
echo "Requests: $REQUESTS ($CONCURRENCY concurrent)"

# Pre-check: server alive
if ! curl -sf "$BASE/health" >/dev/null 2>&1; then
    echo "ERROR: Server not reachable at $BASE/health"
    exit 1
fi

echo "Starting load test..."

# Phase 1: Health checks (baseline)
echo "--- Health checks ($REQUESTS req) ---"
for i in $(seq 1 "$REQUESTS"); do
    run_curl GET "$BASE/health" '{}' "health"
done

# Phase 2: Store entities (writes)
echo "--- Store ($REQUESTS req) ---"
for i in $(seq 1 "$REQUESTS"); do
    id="lt-$i-$$"
    run_curl POST "$BASE/store" \
        "{\"id\":\"$id\",\"category\":\"world\",\"content\":\"Load test entity $i\"}" \
        "store-$i" &
    if (( i % CONCURRENCY == 0 )); then wait; fi
done
wait

# Phase 3: Search queries (reads)
echo "--- Search ($REQUESTS req) ---"
for i in $(seq 1 "$REQUESTS"); do
    run_curl POST "$BASE/search" \
        '{"query":"load test entity","top_k":5}' \
        "search-$i" &
    if (( i % CONCURRENCY == 0 )); then wait; fi
done
wait

echo "--- Retrieve ($REQUESTS req) ---"
for i in $(seq 1 "$REQUESTS"); do
    run_curl POST "$BASE/retrieve" \
        "{\"seed_ids\":[\"lt-1-$$\"],\"max_depth\":2}" \
        "retrieve-$i" &
    if (( i % CONCURRENCY == 0 )); then wait; fi
done
wait

echo "--- Query ($REQUESTS req) ---"
for i in $(seq 1 "$REQUESTS"); do
    run_curl POST "$BASE/query" \
        '{"query":"load test entity","top_k":3}' \
        "query-$i" &
    if (( i % CONCURRENCY == 0 )); then wait; fi
done
wait
