#!/usr/bin/env bash
set -euo pipefail

fail=0
check_forbidden() {
  local path=$1 pattern=$2 message=$3
  if grep -RInE "$pattern" "$path" --include='*.go' >/dev/null 2>&1; then
    echo "boundary violation: $message" >&2
    grep -RInE "$pattern" "$path" --include='*.go' >&2 || true
    fail=1
  fi
}

# Patterns match quoted import paths only, so prose comments that mention
# internal package names (e.g. migration notes) do not trip the gate.
MOD='github\.com/pavelveter/hermem'
check_forbidden pkg/domain "\"$MOD/pkg/spi\"|\"$MOD/src/internal|\"$MOD/api/" 'domain must not depend on SPI, internal, or transport'
check_forbidden pkg/spi "\"$MOD/src/internal|\"$MOD/api/" 'SPI must not depend on internal or transport'
check_forbidden api/v1 "\"$MOD/src/internal" 'v1 DTOs must not depend on internal packages'

if grep -RInE '(^|[^[:alnum:]_])init\(\)' src/internal/app src/internal/vector --include='*.go' >/dev/null 2>&1; then
  echo 'boundary violation: provider composition must not rely on init()' >&2
  fail=1
fi

exit "$fail"
