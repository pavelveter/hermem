#!/bin/bash
# ensure-embed-placeholders.sh — Create placeholder files for go:embed
# so the binary compiles even without the real llama-embedding binary.
#
# Usage: scripts/ensure-embed-placeholders.sh [--force]
#   --force  Overwrite existing bin/ directory

set -euo pipefail

BIN_DIR="src/internal/ai/bin"
LLAMA_BINARY="$BIN_DIR/llama-embedding"
LLAMA_LIBS="$BIN_DIR/llama-libs"

LIBS=(
  libllama-common.0.dylib
  libllama.0.dylib
  libggml.0.dylib
  libggml-cpu.0.dylib
  libggml-blas.0.dylib
  libggml-metal.0.dylib
  libggml-base.0.dylib
)

FORCE=false
if [ "${1:-}" = "--force" ]; then
  FORCE=true
fi

if [ -d "$BIN_DIR" ] && [ "$FORCE" = false ]; then
  echo "→ bin/ already exists, skipping placeholder creation"
  exit 0
fi

echo "→ Creating placeholder files in $BIN_DIR"
mkdir -p "$LLAMA_LIBS"
echo "placeholder" > "$LLAMA_BINARY"
for lib in "${LIBS[@]}"; do
  touch "$LLAMA_LIBS/$lib"
done
