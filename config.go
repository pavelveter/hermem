package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Provider     string
	URL          string
	Key          string
	Model        string
	DBPath             string
	ExtractModel       string
	ExtractTemperature float32
	// DedupThreshold is the cosine-similarity floor above which an
	// incoming entity is considered a duplicate of an existing one and
	// is merged rather than inserted. Cosine similarity lives in [0, 1]
	// (unit-length dot product); 0.88 means "very close in vector space"
	// and is empirically a good default for short factual text.
	// Lower for noisier inputs; raise to merge only near-duplicates.
	DedupThreshold float32
	// MaxDepthCeiling is the hard upper bound on requested graph-walk
	// depth. Calls asking for a larger depth get silently clamped so a
	// pathological request cannot blow up the server. 0 disables the cap.
	MaxDepthCeiling int
	// MaxRetrievedNodes is a soft cap on the total nodes returned by a
	// single RetrieveContext call, protecting response size and memory
	// against dense graph walks. 0 disables the cap.
	MaxRetrievedNodes int
}

// LoadConfig parses hermem.ini from `path` exactly as given — no
// resolution to the binary's directory. Production entry points
// (server, CLI main) should call LoadConfigFromBinaryDir instead;
// this lower-level helper is preserved so tests can inject a known
// path without faking os.Executable(). A bare filename like
// "hermem.ini" here is CWD-relative — that's the footgun this
// helper exists to surface, not to fix.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Provider:          "ollama",
		URL:               "http://localhost:11434",
		Model:             "nomic-embed-text",
		DBPath:            "hermem.db",
		ExtractModel:       "qwen2.5-coder:7b",
		ExtractTemperature: 0.1,
		DedupThreshold:     0.88,
		MaxDepthCeiling:   5,
		MaxRetrievedNodes: 100,
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	section := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}

		if line[0] == '[' && line[len(line)-1] == ']' {
			section = line[1 : len(line)-1]
			continue
		}

		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])

		switch strings.ToLower(section + "." + key) {
		case "embedder.provider":
			cfg.Provider = strings.ToLower(val)
		case "embedder.url":
			cfg.URL = val
		case "embedder.key":
			cfg.Key = val
		case "embedder.model":
			cfg.Model = val
		case "database.path":
			cfg.DBPath = val
		case "extraction.model":
			cfg.ExtractModel = val
		case "extraction.temperature":
			if v, err := strconv.ParseFloat(val, 32); err == nil {
				cfg.ExtractTemperature = float32(v)
			} else {
				log.Printf("config: invalid extraction.temperature %q, keeping default %.2f: %v", val, cfg.ExtractTemperature, err)
			}
		case "ingestion.dedup_threshold":
			if v, err := strconv.ParseFloat(val, 32); err == nil {
				cfg.DedupThreshold = float32(v)
			} else {
				log.Printf("config: invalid ingestion.dedup_threshold %q, keeping default %.2f: %v", val, cfg.DedupThreshold, err)
			}
		case "retrieval.depth_ceiling":
			if v, err := strconv.Atoi(val); err == nil && v >= 0 {
				cfg.MaxDepthCeiling = v
			} else {
				log.Printf("config: invalid retrieval.depth_ceiling %q, keeping default %d: %v", val, cfg.MaxDepthCeiling, err)
			}
		case "retrieval.max_nodes":
			if v, err := strconv.Atoi(val); err == nil && v >= 0 {
				cfg.MaxRetrievedNodes = v
			} else {
				log.Printf("config: invalid retrieval.max_nodes %q, keeping default %d: %v", val, cfg.MaxRetrievedNodes, err)
			}
		}
	}

	return cfg, scanner.Err()
}

func (c *Config) NewEmbedder() Embedder {
	switch c.Provider {
	case "openai":
		return NewOpenAIEmbedder(c.URL, c.Key, c.Model)
	default:
		return NewOllamaEmbedder(c.URL, c.Model)
	}
}

func (c *Config) NewExtractor() LLMExtractor {
	return NewOllamaLLMExtractor(c.URL, c.ExtractModel, c.ExtractTemperature)
}

// LoadConfigFromBinaryDir is the production entry point: it resolves
// hermem.ini relative to the currently-running executable via
// os.Executable(), so the binary behaves identically regardless of
// the caller's working directory. A `~/.hermes/bin/hermem store`
// invocation lands the same way whether run from its install
// directory, from a cron job's CWD, or from a fresh shell.
//
// A missing ini triggers the same default-config-on-missing policy
// used by LoadConfig (no error, defaults propagated) so an absent
// file is non-fatal — deployments without a hermem.ini still boot
// with the built-in defaults.
func LoadConfigFromBinaryDir() (*Config, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate executable: %w", err)
	}
	return LoadConfigFromDir(filepath.Dir(exePath))
}

// LoadConfigFromDir loads hermem.ini from `dir`. Exported so tests
// can drive the same code path without faking os.Executable() (the
// stdlib doesn't allow that to be replaced at test time).
func LoadConfigFromDir(dir string) (*Config, error) {
	return LoadConfig(filepath.Join(dir, "hermem.ini"))
}

// resolveDBPath interprets cfg.DBPath in a hermem-binary-aware way:
// absolute paths are returned unchanged so operators can pin the DB
// to /var/lib/hermem/ or similar; relative paths are joined to the
// binary's directory so the DB is colocated with the binary, not
// the caller's working directory.
//
// Note: os.Executable reports the kernel-resolved path, not the
// symlink path. A binary installed via a symlink (e.g.
// /usr/local/bin/hermem -> /opt/hermem-real/hermem) reads its
// ini and writes its DB in /opt/hermem-real/, not /usr/local/bin/.
// We deliberately do NOT follow the symlink: it matches Go stdlib
// semantics and avoids platform-specific os.Readlink logic. If an
// operator needs symlink-following, that's a future PR.
//
// On os.Executable failure the original path is returned unchanged
// so InitDB surfaces the original failure mode rather than masking
// it behind a binary-resolution error.
func resolveDBPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	exePath, err := os.Executable()
	if err != nil {
		return p
	}
	return filepath.Join(filepath.Dir(exePath), p)
}
