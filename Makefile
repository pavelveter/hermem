.PHONY: build build-local build-no-local clean test test-e2e benchmarks lint install sign routes completions install-completions fmt vet test-coverage

BIN_DIR := src/internal/ai/bin
LLAMA_BINARY := $(BIN_DIR)/llama-embedding
LLAMA_LIBS := $(BIN_DIR)/llama-libs

INSTALL_DIR ?= $(HOME)/.local/bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# User-local XDG-style completion install paths. Override per-shell with:
#   make install-completions COMPLETIONS_BASH_DIR=/etc/bash_completion.d
COMPLETIONS_BASH_DIR ?= $(HOME)/.local/share/bash-completion/completions
COMPLETIONS_ZSH_DIR  ?= $(HOME)/.local/share/zsh/site-functions
COMPLETIONS_FISH_DIR ?= $(HOME)/.config/fish/completions

LDFLAGS := -X 'github.com/pavelveter/hermem/api.BuildVersion=$(VERSION)' \
           -X 'main.version=$(VERSION)' \
           -X 'main.buildDate=$(BUILD_DATE)' \
           -X 'main.gitCommit=$(GIT_COMMIT)'

# Default build — works with or without local embedding binary
build:
	@if [ ! -d "$(BIN_DIR)" ]; then \
		scripts/ensure-embed-placeholders.sh; \
	fi
	go build -ldflags "$(LDFLAGS)" -o hermem ./src

# Build with real llama-embedding binary (for local embedding)
build-local: $(LLAMA_BINARY)
	go build -ldflags "$(LDFLAGS)" -o hermem ./src

$(LLAMA_BINARY):
	@echo "Place llama-embedding + dylibs in $(BIN_DIR)/ before building"
	@echo "  cp /path/to/llama-embedding $(LLAMA_BINARY)"
	@echo "  cp /path/to/lib*.dylib $(LLAMA_LIBS)/"
	@exit 1

# Build without local embedding (faster, no CGo deps)
build-no-local:
	go build -ldflags "$(LDFLAGS)" -tags no_local_embedding -o hermem ./src

# Run unit tests
test:
	go test -race -count=1 ./src/...

# Run E2E tests
test-e2e: build
	go test -p 1 ./tests/e2e/... -v -timeout 5m

# Run benchmarks
benchmarks:
	go test -bench=. -benchmem -count=3 ./src/...

# Run linter
lint:
	golangci-lint run ./...

# Re-sign binary with clean ad-hoc signature (fixes "Code Signature Invalid" on macOS)
sign: hermem
	@if [ "$$(uname)" = "Darwin" ]; then \
		codesign --force --sign - hermem && \
		echo "Signed: hermem"; \
	else \
		echo "sign: skipped (not Darwin)"; \
	fi

# Build, sign, and install to ~/.local/bin (override with INSTALL_DIR=...)
install: build
	@mkdir -p "$(INSTALL_DIR)"
	@cp hermem "$(INSTALL_DIR)/hermem"
	@if [ -f hermem.ini ]; then cp hermem.ini "$(INSTALL_DIR)/hermem.ini"; fi
	@if [ "$$(uname)" = "Darwin" ]; then \
		codesign --force --sign - "$(INSTALL_DIR)/hermem" && \
		echo "Signed: $(INSTALL_DIR)/hermem"; \
	fi
	@echo "Installed: $(INSTALL_DIR)/hermem"

clean:
	rm -f hermem
	rm -rf $(BIN_DIR)

# Regenerate route inventory from OpenAPI spec
routes:
	@echo "Route inventory: docs/generated/ROUTES.md"
	@echo "Update manually when routes change — the file is the canonical source."

# Generate shell completion scripts
completions: build
	@mkdir -p completions
	./hermem completion bash > completions/hermem.bash
	./hermem completion zsh > completions/hermem.zsh
	./hermem completion fish > completions/hermem.fish
	@echo "Generated: completions/hermem.{bash,zsh,fish}"

# Install shell completions to user-local XDG paths.
# Overridable: COMPLETIONS_BASH_DIR, COMPLETIONS_ZSH_DIR, COMPLETIONS_FISH_DIR.
# Depends on `completions` so binaries without a Go toolchain can still
# install pre-generated files when the directory already exists.
install-completions:
	@if [ ! -d completions ]; then \
		echo "completions/ directory missing — run 'make completions' first" >&2; \
		exit 1; \
	fi
	@mkdir -p $(COMPLETIONS_BASH_DIR)
	@cp completions/hermem.bash $(COMPLETIONS_BASH_DIR)/hermem
	@mkdir -p $(COMPLETIONS_ZSH_DIR)
	@cp completions/hermem.zsh $(COMPLETIONS_ZSH_DIR)/_hermem
	@mkdir -p $(COMPLETIONS_FISH_DIR)
	@cp completions/hermem.fish $(COMPLETIONS_FISH_DIR)/hermem.fish
	@echo "Installed shell completions:"
	@echo "  bash: $(COMPLETIONS_BASH_DIR)/hermem"
	@echo "  zsh : $(COMPLETIONS_ZSH_DIR)/_hermem"
	@echo "  fish: $(COMPLETIONS_FISH_DIR)/hermem.fish"

# Format code
fmt:
	gofmt -s -w .

# Run go vet
vet:
	go vet ./src/...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out -covermode=atomic ./src/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"
