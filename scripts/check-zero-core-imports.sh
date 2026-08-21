#!/usr/bin/env bash
set -euo pipefail

# Gate: no production code may import src/internal/core once the facade is
# removed. During the compatibility window the facade still exists and this
# gate is intentionally non-enforcing: the "Forbid new production core
# imports" guard in CI already prevents NEW callers, while this stricter
# check becomes the pass/fail boundary at the breaking-removal PR.
#
# Activation contract: the breaking-removal PR deletes src/internal/core and
# this script then fails if any production (non _test) Go file still imports
# it. Until the directory is removed, it exits 0 with an explanatory message.

if [ -d src/internal/core ]; then
  echo "core facade still present; zero-import gate inactive (enforced after removal)"
  exit 0
fi

violations=$(grep -RInE '"github\.com/pavelveter/hermem/src/internal/core"' \
  src --include='*.go' | grep -v '_test\.go:' || true)

if [ -n "$violations" ]; then
  echo "::error::production code still imports the removed src/internal/core facade" >&2
  echo "$violations" >&2
  exit 1
fi

echo "No production core imports found — OK"
