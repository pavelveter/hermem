#!/bin/bash
set -e

# Hermem installer for Hermes Agent
# Usage: curl -fsSL https://raw.githubusercontent.com/hermem/hermem/main/install.sh | bash

HERMES_HOME="${HERMES_HOME:-$HOME/.hermes}"
HERMEM_BIN_DIR="$HERMES_HOME/bin"
HERMEM_PLUGIN_DIR="$HERMES_HOME/hermes-agent/plugins/memory/hermem"
HERMEM_CONFIG="$HERMES_HOME/hermem.ini"
HERMES_CONFIG="$HERMES_HOME/config.yaml"

echo "Installing Hermem memory provider..."

# Check Go
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed. Install from https://go.dev/dl/"
    exit 1
fi

# Check CGO
if [ "$(go env CGO_ENABLED)" != "1" ]; then
    echo "Error: CGO is required. Set CGO_ENABLED=1"
    exit 1
fi

# Build binary
echo "Building hermem..."
go build -o hermem .

# Install binary
echo "Installing binary to $HERMEM_BIN_DIR..."
mkdir -p "$HERMEM_BIN_DIR"
cp hermem "$HERMEM_BIN_DIR/"
chmod +x "$HERMEM_BIN_DIR/hermem"

# Install plugin
echo "Installing plugin to $HERMEM_PLUGIN_DIR..."
mkdir -p "$HERMEM_PLUGIN_DIR"
cp plugins/memory/hermem/__init__.py "$HERMEM_PLUGIN_DIR/"
cp plugins/memory/hermem/plugin.yaml "$HERMEM_PLUGIN_DIR/"

# Install config (don't overwrite)
if [ ! -f "$HERMEM_CONFIG" ]; then
    echo "Installing config to $HERMEM_CONFIG..."
    cp hermem.ini "$HERMEM_CONFIG"
else
    echo "Config exists at $HERMEM_CONFIG, skipping..."
fi

# Set provider in config.yaml
if [ -f "$HERMES_CONFIG" ]; then
    if grep -q "provider: hermem" "$HERMES_CONFIG"; then
        echo "Provider already set to hermem"
    else
        echo "Setting memory provider to hermem..."
        sed -i.bak 's/^  provider:.*/  provider: hermem/' "$HERMES_CONFIG"
        rm -f "$HERMES_CONFIG.bak"
    fi
else
    echo "Warning: $HERMES_CONFIG not found. Set memory.provider: hermem manually."
fi

# Add to PATH if needed
if [[ ":$PATH:" != *":$HERMEM_BIN_DIR:"* ]]; then
    echo ""
    echo "Add to your shell profile:"
    echo "  export PATH=\"\$HOME/.hermes/bin:\$PATH\""
fi

echo ""
echo "✓ Hermem installed successfully!"
echo ""
echo "Next steps:"
echo "  1. Restart Hermes: hermes gateway restart"
echo "  2. Verify: hermes memory"
echo "  3. Configure embedder: edit $HERMEM_CONFIG"
echo ""
echo "CLI usage:"
echo "  echo '{\"query\":\"test\"}' | hermem query"
echo "  hermem serve 8420"
