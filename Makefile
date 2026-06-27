.PHONY: build build-local build-no-local clean

BIN_DIR := src/internal/ai/bin
LLAMA_BINARY := $(BIN_DIR)/llama-embedding
LLAMA_LIBS := $(BIN_DIR)/llama-libs

# Default build — works with or without local embedding binary
build:
	@if [ ! -d "$(BIN_DIR)" ]; then \
		echo "→ bin/ not found, creating placeholder for go:embed"; \
		mkdir -p $(LLAMA_LIBS); \
		touch $(LLAMA_BINARY); \
		echo "placeholder" > $(LLAMA_BINARY); \
		for lib in libllama-common.0.dylib libllama.0.dylib libggml.0.dylib libggml-cpu.0.dylib libggml-blas.0.dylib libggml-metal.0.dylib libggml-base.0.dylib; do \
			touch $(LLAMA_LIBS)/$$lib; \
		done; \
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
