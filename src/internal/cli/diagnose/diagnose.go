// Package diagnose implements the `hermem diagnose` CLI subcommand —
// a self-check of the running database and memory subsystem that emits
// a structured health report.
//
// Usage:
//
//	hermem diagnose              # human-readable output
//	hermem diagnose --json       # machine-readable JSON
package diagnose

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/cli/diagnose/checks"
	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

// NewCmd returns the diagnose cobra subcommand.
func NewCmd(env *clienv.Env) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Run self-diagnostics on the database and memory subsystem",
		Long: `Run self-diagnostics on the hermem database and memory subsystem.

Checks performed:
  - Schema integrity: foreign-key violations, orphan edges, PRAGMA integrity_check.
  - Vector index: id_map row count, embedding dimension consistency.
  - Memory subsystem: embedding density by category, beliefs table by status.
  - Retention state: archived entity count.
  - Recent errors: tail (empty if no log access).

Output is human-readable by default; pass --json for machine-readable JSON.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDiagnose(env, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output machine-readable JSON instead of human text")
	return cmd
}

func runDiagnose(env *clienv.Env, jsonOutput bool) error {
	db := env.DB

	schema, err := checks.CheckSchema(db)
	if err != nil {
		return fmt.Errorf("schema check: %w", err)
	}

	configuredDim := 0
	if env.Cfg != nil {
		configuredDim = env.Cfg.VectorDim
	}
	vector, err := checks.CheckVector(db, configuredDim)
	if err != nil {
		return fmt.Errorf("vector check: %w", err)
	}

	memory, err := checks.CheckMemory(db)
	if err != nil {
		return fmt.Errorf("memory check: %w", err)
	}

	retention, err := checks.CheckRetention(db)
	if err != nil {
		return fmt.Errorf("retention check: %w", err)
	}

	errors := checks.CheckErrorsTail()

	report := &Report{
		Schema:    toReportSchema(schema),
		Vector:    toReportVector(vector),
		Memory:    toReportMemory(memory),
		Retention: toReportRetention(retention),
		Errors:    toReportErrors(errors),
	}

	if jsonOutput {
		os.Stdout.Write(report.JSON())
		os.Stdout.Write([]byte("\n"))
		return nil
	}
	printHuman(report)
	return nil
}

func toReportSchema(s checks.SchemaReport) SchemaReport {
	return SchemaReport{
		ForeignKeysOK: s.ForeignKeysOK,
		OrphanEdges:   s.OrphanEdges,
		IntegrityOK:   s.IntegrityOK,
		IntegrityLog:  s.IntegrityLog,
	}
}

func toReportVector(v checks.VectorReport) VectorReport {
	return VectorReport{
		TotalRows:    v.TotalRows,
		ConfigDim:    v.ConfigDim,
		StoredDim:    v.StoredDim,
		DimMismatch:  v.DimMismatch,
		CategoryDims: v.CategoryDims,
	}
}

func toReportMemory(m checks.MemoryReport) MemoryReport {
	return MemoryReport{
		TotalEntities:         m.TotalEntities,
		EntitiesWithEmbedding: m.EntitiesWithEmbedding,
		EmbeddingDensity:      m.EmbeddingDensity,
		DensityByCategory:     m.DensityByCategory,
		BeliefCounts:          m.BeliefCounts,
	}
}

func toReportRetention(r checks.RetentionReport) RetentionReport {
	return RetentionReport{
		ArchivedEntities: r.ArchivedEntities,
		TotalEntities:    r.TotalEntities,
		ArchivedPct:      r.ArchivedPct,
	}
}

func toReportErrors(e checks.ErrorsReport) ErrorsReport {
	return ErrorsReport{
		Entries: e.Entries,
		Note:    e.Note,
	}
}

func printHuman(r *Report) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "== Schema Integrity ==")
	fmt.Fprintf(w, "Foreign keys OK:\t%v\n", r.Schema.ForeignKeysOK)
	fmt.Fprintf(w, "Orphan edges:\t%d\n", r.Schema.OrphanEdges)
	fmt.Fprintf(w, "Integrity check OK:\t%v\n", r.Schema.IntegrityOK)
	for _, l := range r.Schema.IntegrityLog {
		fmt.Fprintf(w, "  ! %s\n", l)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "== Vector Index ==")
	fmt.Fprintf(w, "Total rows (id_map):\t%d\n", r.Vector.TotalRows)
	fmt.Fprintf(w, "Config dimension:\t%d\n", r.Vector.ConfigDim)
	fmt.Fprintf(w, "Stored dimension:\t%d\n", r.Vector.StoredDim)
	fmt.Fprintf(w, "Dimension mismatch:\t%v\n", r.Vector.DimMismatch)

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "== Memory Subsystem ==")
	fmt.Fprintf(w, "Total entities:\t%d\n", r.Memory.TotalEntities)
	fmt.Fprintf(w, "With embedding:\t%d\n", r.Memory.EntitiesWithEmbedding)
	fmt.Fprintf(w, "Embedding density:\t%.1f%%\n", r.Memory.EmbeddingDensity)
	for cat, pct := range r.Memory.DensityByCategory {
		fmt.Fprintf(w, "  Density [%s]:\t%.1f%%\n", cat, pct)
	}
	if len(r.Memory.BeliefCounts) > 0 {
		fmt.Fprintln(w, "Beliefs by status:")
		for st, cnt := range r.Memory.BeliefCounts {
			fmt.Fprintf(w, "  %s:\t%d\n", st, cnt)
		}
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "== Retention ==")
	fmt.Fprintf(w, "Archived entities:\t%d / %d (%.1f%%)\n", r.Retention.ArchivedEntities, r.Retention.TotalEntities, r.Retention.ArchivedPct)

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "== Recent Errors ==")
	if len(r.Errors.Entries) == 0 {
		fmt.Fprintf(w, "Note:\t%s\n", r.Errors.Note)
	} else {
		for _, e := range r.Errors.Entries {
			fmt.Fprintln(w, e)
		}
	}
}
