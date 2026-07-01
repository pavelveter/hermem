package adminops

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/pavelveter/hermem/src/internal/admin"
	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

func newVacuumCmd(env *cli.Env) *cobra.Command {
	var noProgress bool
	cmd := &cobra.Command{
		Use:   "vacuum",
		Short: "Run SQLite VACUUM to reclaim disk space",
		Long: `Rebuild the database file to reclaim unused disk space.

SQLite databases accumulate free pages over time from deletes and
updates. VACUUM rebuilds the entire database file, removing gaps
and reducing file size.

⚠ VACUUM requires temporary disk space roughly equal to the current
database size. It also acquires an exclusive lock — no other writers
can access the DB during the operation.

Flags:
  --no-progress    Suppress progress output

Output:
  VACUUM progress: 45%
  VACUUM complete — 12.3 MB reclaimed

Progress updates are printed to stdout (unless --no-progress).
The final line reports the total space reclaimed.

Examples:
  hermem ops vacuum
  hermem ops vacuum --no-progress`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vr := admin.NewVacuumRunner(env.DB)

			if !noProgress {
				var (
					mu      sync.Mutex
					lastPct int
				)
				vr.OnProgress(func(pct int, reclaimed int64) {
					mu.Lock()
					lastPct = pct
					mu.Unlock()
				})
				// Print progress in background
				ctx, cancel := context.WithCancel(cmd.Context())
				defer cancel()
				go func() {
					ticker := time.NewTicker(1 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							mu.Lock()
							p := lastPct
							mu.Unlock()
							if p > 0 {
								progress := p
								fmt.Fprintf(cmd.OutOrStdout(), "\rVACUUM progress: %d%%", progress)
							}
						}
					}
				}()
			}

			reclaimed, err := vr.Run(cmd.Context())
			if err != nil {
				return err
			}
			if !noProgress {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "VACUUM complete — %s reclaimed\n", byteSize(reclaimed))
			return nil
		},
	}
	cmd.Flags().BoolVar(&noProgress, "no-progress", false, "suppress progress output")
	return cmd
}
