package ai

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed bin/llama-embedding
var llamaBinary []byte

//go:embed bin/llama-libs
var llamaLibFS embed.FS

// LocalEmbedder implements core.Embedder by invoking the llama-embedding binary.
// The binary and its dylibs are embedded via go:embed and extracted to a temp
// directory on first use. Thread-safe: concurrent Embed calls serialize via mutex.
type LocalEmbedder struct {
	binPath string
	dir     string
	model   string
	timeout time.Duration
	mu      sync.Mutex
	once    sync.Once
	initErr error
}

// NewLocalEmbedder creates a LocalEmbedder that shells out to the embedded
// llama-embedding binary. modelPath must point to a .gguf file on disk.
func NewLocalEmbedder(modelPath string, timeout time.Duration) (*LocalEmbedder, error) {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("local embed: model not found: %w", err)
	}
	return &LocalEmbedder{
		model:   modelPath,
		timeout: timeout,
	}, nil
}

// setup extracts the embedded binary and libs to a temp directory.
func (e *LocalEmbedder) setup() error {
	e.once.Do(func() {
		dir, err := os.MkdirTemp("", "hermem-llama-*")
		if err != nil {
			e.initErr = fmt.Errorf("local embed: create temp dir: %w", err)
			return
		}
		e.dir = dir

		// Write binary.
		binPath := filepath.Join(dir, "llama-embedding")
		if err := os.WriteFile(binPath, llamaBinary, 0755); err != nil {
			e.initErr = fmt.Errorf("local embed: write binary: %w", err)
			return
		}
		e.binPath = binPath

		// Extract libs from embedded FS.
		libsDir := filepath.Join(dir, "libs")
		if err := os.MkdirAll(libsDir, 0755); err != nil {
			e.initErr = fmt.Errorf("local embed: create libs dir: %w", err)
			return
		}

		err = fs.WalkDir(llamaLibFS, "bin/llama-libs", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, err := llamaLibFS.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			dest := filepath.Join(libsDir, d.Name())
			return os.WriteFile(dest, data, 0755)
		})
		if err != nil {
			e.initErr = fmt.Errorf("local embed: extract libs: %w", err)
			return
		}

		// Fix rpath to point to our libs directory.
		cmd := exec.Command("install_name_tool", "-rpath",
			"/Users/pavelveter/Projects/labs/hermem-workspace/llama.cpp/build/bin",
			libsDir, binPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			e.initErr = fmt.Errorf("local embed: fix rpath: %w: %s", err, out)
			return
		}

		// Fix each dylib's ID to use @rpath.
		libNames := []string{
			"libllama-common.0.dylib",
			"libllama.0.dylib",
			"libggml.0.dylib",
			"libggml-cpu.0.dylib",
			"libggml-blas.0.dylib",
			"libggml-metal.0.dylib",
			"libggml-base.0.dylib",
		}
		for _, name := range libNames {
			libPath := filepath.Join(libsDir, name)
			cmd := exec.Command("install_name_tool", "-id", "@rpath/"+name, libPath)
			if out, err := cmd.CombinedOutput(); err != nil {
				e.initErr = fmt.Errorf("local embed: fix lib id %s: %w: %s", name, err, out)
				return
			}
		}
	})
	return e.initErr
}

// Embed returns the embedding vector for text by invoking llama-embedding.
func (e *LocalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.setup(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.binPath,
		"-m", e.model,
		"-p", text,
		"--embd-output-format", "raw",
		"--embd-normalize", "2",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("local embed: %w: %s", err, stderr.String())
	}

	return parseRawEmbedding(stdout.String())
}

// parseRawEmbedding parses whitespace-separated floats from llama-embedding raw output.
func parseRawEmbedding(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("local embed: empty output")
	}

	fields := strings.Fields(s)
	vec := make([]float32, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 32)
		if err != nil {
			return nil, fmt.Errorf("local embed: parse float %q: %w", f, err)
		}
		vec[i] = float32(v)
	}
	return vec, nil
}
