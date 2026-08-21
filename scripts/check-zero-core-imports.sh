#!/usr/bin/env bash
set -euo pipefail

# Gate: production (non _test) code must not depend on the src/internal/core
# facade. Two modes:
#
#   1. Facade present (compatibility window): importers are allowed ONLY
#      inside the legacy-vector compatibility surface — src/internal/spiadapter
#      and src/internal/vector — which adapts the IDs-only backends to the
#      public spi.VectorStore contract and is deleted by task 6.4/6.5.
#      Any other production importer fails this gate immediately.
#
#   2. Facade removed (post breaking-removal PR): zero production imports,
#      no allowlist.

ALLOWLIST_PREFIXES=(
  "src/internal/spiadapter/"
  "src/internal/vector/"
)

violations=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  case "$f" in
    src/internal/core/*) continue ;; # the facade itself
  esac
  if [ ! -d src/internal/core ]; then
    violations+=" $f"$'\n'
    continue
  fi
  allowed=0
  for prefix in "${ALLOWLIST_PREFIXES[@]}"; do
    case "$f" in
      "$prefix"*) allowed=1; break ;;
    esac
  done
  if [ "$allowed" -eq 0 ]; then
    violations+=" $f"$'\n'
  fi
done < <(grep -RlE '"github\.com/pavelveter/hermem/src/internal/core"' \
  src --include='*.go' 2>/dev/null | grep -v '_test\.go$' || true)

if [ -n "$violations" ]; then
  if [ -d src/internal/core ]; then
    echo "::error::production code outside the compat surface imports src/internal/core:" >&2
  else
    echo "::error::production code still imports the removed src/internal/core facade:" >&2
  fi
  printf '%s' "$violations" >&2
  exit 1
fi

if [ -d src/internal/core ]; then
  echo "core imports confined to the legacy-vector compat surface — OK"
else
  echo "No production core imports found — OK"
fi
