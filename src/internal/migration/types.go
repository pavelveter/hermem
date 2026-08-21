package migration

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/store"
)

// Status mirrors store.MigStatus for the migration domain service.
// It is the transport-agnostic view of one applied/pending migration.
type Status struct {
	Name           string `json:"name"`
	Applied        bool   `json:"applied"`
	AppliedAt      string `json:"applied_at,omitempty"`
	ChecksumSHA256 string `json:"checksum_sha256"`
	ChecksumMatch  *bool  `json:"checksum_match,omitempty"`
}

// Mismatch mirrors store.MigMismatch for the migration domain service.
type Mismatch struct {
	Name            string `json:"name"`
	StoredChecksum  string `json:"stored_checksum"`
	CurrentChecksum string `json:"current_checksum"`
}

// Migrator is the minimal interface for migration operations.
// Service satisfies this interface. CLI commands and HTTP shells depend
// on Migrator (or *Service) rather than calling store functions directly.
type Migrator interface {
	// Run applies all pending migrations and returns the post-apply status.
	Run(ctx context.Context) ([]Status, error)
	// DryRun returns pending migrations without applying them.
	DryRun(ctx context.Context) ([]Status, error)
	// Status returns the applied/pending state of every migration.
	Status(ctx context.Context) ([]Status, error)
	// Verify returns migrations whose stored checksum diverges from the
	// current embedded file. Empty result = no drift.
	Verify(ctx context.Context) ([]Mismatch, error)
	// Rollback removes applied migrations back to the target version.
	// Empty target rolls back the last applied migration.
	Rollback(ctx context.Context, target string) (string, error)
}

// toStatus converts store migration rows into the domain Status view,
// carrying every field verbatim (including the nullable ChecksumMatch
// pointer) so the store→migration ownership move drops no wire-visible
// data.
func toStatus(in []store.MigStatus) []Status {
	out := make([]Status, len(in))
	for i, s := range in {
		out[i] = Status{
			Name:           s.Name,
			Applied:        s.Applied,
			AppliedAt:      s.AppliedAt,
			ChecksumSHA256: s.ChecksumSHA256,
			ChecksumMatch:  s.ChecksumMatch,
		}
	}
	return out
}

// toMismatch converts store integrity rows into the domain Mismatch view.
func toMismatch(in []store.MigMismatch) []Mismatch {
	out := make([]Mismatch, len(in))
	for i, m := range in {
		out[i] = Mismatch{
			Name:            m.Name,
			StoredChecksum:  m.StoredChecksum,
			CurrentChecksum: m.CurrentChecksum,
		}
	}
	return out
}
