.PHONY: build build-local build-no-local clean test test-e2e benchmarks lint

BIN_DIR := src/internal/ai/bin
LLAMA_BINARY := $(BIN_DIR)/llama-embedding
LLAMA_LIBS := $(BIN_DIR)/llama-libs

# Default build — works with or without local embedding binary
build:
	@if [ ! -d "$(BIN_DIR)" ]; then \
		scripts/ensure-embed-placeholders.sh; \
	fi
	go build -o hermem ./src

# Build with real llama-embedding binary (for local embedding)
build-local: $(LLAMA_BINARY)
	go build -o hermem ./src

$(LLAMA_BINARY):
	@echo "Place llama-embedding + dylibs in $(BIN_DIR)/ before building"
	@echo "  cp /path/to/llama-embedding $(LLAMA_BINARY)"
	@echo "  cp /path/to/lib*.dylib $(LLAMA_LIBS)/"
	@exit 1

# Build without local embedding (faster, no CGo deps)
build-no-local:
	go build -tags no_local_embedding -o hermem ./src

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

clean:
	rm -f hermem
	rm -rf $(BIN_DIR)
