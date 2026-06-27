package ingestion

import (
	"database/sql"

	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion/detectors"
)

// IngestionWorkerConfig holds all dependencies for creating an IngestionWorker.
type IngestionWorkerConfig struct {
	DB             *sql.DB
	VectorIndex    core.VectorIndex
	Extractor      core.LLMExtractor
	Embedder       core.Embedder
	DedupThreshold float32
	Schema         core.SchemaConfig
	Detector       contradiction.ContradictionDetector
}

// NewIngestionWorkerFromConfig creates a worker from a config struct.
// If Detector is nil, falls back to the default lexical detector.
func NewIngestionWorkerFromConfig(cfg IngestionWorkerConfig) *IngestionWorker {
	if cfg.Detector == nil {
		cfg.Detector = detectors.NewLexicalDetector()
	}
	return &IngestionWorker{
		db:          cfg.DB,
		vi:          cfg.VectorIndex,
		extractor:   cfg.Extractor,
		embedder:    cfg.Embedder,
		dedupThresh: cfg.DedupThreshold,
		schema:      cfg.Schema,
		detector:    cfg.Detector,
	}
}

// MemoryWorkerConfig holds all dependencies for MemoryWorker and MemoryWorkerResilient.
type MemoryWorkerConfig struct {
	DB             *sql.DB
	VectorIndex    core.VectorIndex
	Extractor      core.LLMExtractor
	Embedder       core.Embedder
	DedupThreshold float32
	Schema         core.SchemaConfig
	CkptPath       string
	PendingPath    string
	WorkerID       string
}
