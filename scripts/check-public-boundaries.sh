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

check_forbidden pkg/domain 'pkg/spi|src/internal|/api(/|\")' 'domain must not depend on SPI, internal, or transport'
check_forbidden pkg/spi 'src/internal|/api(/|\")' 'SPI must not depend on internal or transport'
check_forbidden api/v1 'src/internal/core|src/internal/' 'v1 DTOs must not depend on internal packages'
# The legacy interfaces remain defined for the compatibility release. The
# guard is intentionally limited to public-package direction and provider
# composition; removing the facade is a later breaking-release gate.

if grep -RInE '(^|[^[:alnum:]_])init\(\)' src/internal/app src/internal/vector --include='*.go' >/dev/null 2>&1; then
  echo 'boundary violation: provider composition must not rely on init()' >&2
  fail=1
fi

exit "$fail"
