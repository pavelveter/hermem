package health

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

func DBProbe(db *sql.DB) Check {
	return Check{
		Name: "database",
		Probe: func(ctx context.Context) error {
			return db.PingContext(ctx)
		},
		Timeout:  5 * time.Second,
		Severity: "critical",
	}
}

func VectorIndexProbe(vi core.VectorIndex, opts ...int) Check {
	dim := 768
	if len(opts) > 0 && opts[0] > 0 {
		dim = opts[0]
	}
	return Check{
		Name: "vector_index",
		Probe: func(ctx context.Context) error {
			if vi == nil {
				return errors.New("vector index is nil")
			}
			vec := make([]float32, dim)
			_, err := vi.Search(ctx, vec, 1)
			return err
		},
		Timeout:  5 * time.Second,
		Severity: "critical",
	}
}

func EmbedderProbe(em core.Embedder) Check {
	return Check{
		Name: "embedder",
		Probe: func(ctx context.Context) error {
			if em == nil {
				return errors.New("embedder is nil")
			}
			vec, err := em.Embed(ctx, "ping")
			if err != nil {
				return fmt.Errorf("embed ping: %w", err)
			}
			if len(vec) == 0 {
				return errors.New("embedding returned zero-length vector")
			}
			return nil
		},
		Timeout:  10 * time.Second,
		Severity: "critical",
	}
}

func ExtractorProbe(ex core.LLMExtractor) Check {
	return Check{
		Name: "extractor",
		Probe: func(ctx context.Context) error {
			if ex == nil {
				return errors.New("extractor is nil or not configured")
			}
			_, err := ex.ExtractEntities(ctx, "ping")
			return err
		},
		Timeout:  10 * time.Second,
		Severity: "warning",
	}
}

func DiskSpaceProbe(path string) Check {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	return Check{
		Name: "disk_space",
		Probe: func(ctx context.Context) error {
			var stat syscall.Statfs_t
			if err := syscall.Statfs(dir, &stat); err != nil {
				return fmt.Errorf("statfs: %w", err)
			}
			freeBytes := stat.Bavail * uint64(stat.Bsize)
			const minFree = 100 * 1024 * 1024
			if freeBytes < minFree {
				return fmt.Errorf("low disk space: %d bytes free, need %d", freeBytes, minFree)
			}
			return nil
		},
		Timeout:  5 * time.Second,
		Severity: "critical",
	}
}
