.PHONY: build build-local build-no-local clean

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

clean:
	rm -f hermem
	rm -rf $(BIN_DIR)
