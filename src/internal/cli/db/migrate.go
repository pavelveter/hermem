package db

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/migration"
	"github.com/pavelveter/hermem/src/internal/store"
)

// newMigrateCmd is the parent of the migration subcommands.
//
// §4 audit closure: `hermem db migrate apply` is the post-§4 way to
// advance schema OUTSIDE the boot sequence — recommended in K8s
// InitContainers or any pre-deploy step so a long-running migration
// never holds the daemon's start gate open (which would let liveness/
// readiness probes kill the pod mid-ALTER → CrashLoopBackOff on a
// half-migrated DB).
//
// Bare `hermem db migrate` still prints status (backward-compatible
// with the pre-§4 invocation; same output as `migrate status`).
func newMigrateCmd(env *cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage schema migrations (status / apply)",
		Long: `Manage schema migrations.

  hermem db migrate         # status (default, backward-compatible)
  hermem db migrate status  # applied / pending for every migration file
  hermem db migrate apply   # apply every pending migration now and exit

Apply is the post-§4 replacement for the apply-on-boot semantic.
Production should run it in an InitContainer; the daemon refuses
to boot against an out-of-date schema unless [database]
auto_migrate = true is set in hermem.ini.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Bare `migrate` → status (backward compat).
			return runMigrateStatus(cmd, env)
		},
	}
	cmd.AddCommand(newMigrateStatusCmd(env))
	cmd.AddCommand(newMigrateApplyCmd(env))
	return cmd
}

// newMigrateStatusCmd prints applied / pending state for every
// embedded migration file. Also wired as the bare-`migrate` handler
// for backward compat with the pre-§4 invocation pattern.
func newMigrateStatusCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show applied / pending state of every migration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrateStatus(cmd, env)
		},
	}
}

// newMigrateApplyCmd applies every pending migration now and exits.
// Designed for K8s InitContainers, pre-deploy scripts, and operator
// escalations that need to advance schema without restarting the
// daemon.
//
// Output prints the headline "applied N" delta by computing the
// pre-apply DryRun snapshot vs the post-apply Status snapshot —
// no special store-layer plumbing required. When the DB is already
// up-to-date, prints "Already up-to-date." with exit 0 so a smoke
// script can re-invoke safely.
func newMigrateApplyCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Apply every pending migration now and exit",
		Long: `Apply every pending migration now and exit.

This is the post-§4 replacement for the pre-§4 apply-on-boot
behaviour. Production: run it in a K8s InitContainer, then start the
daemon. Dev: set '[database] auto_migrate = true' in hermem.ini to
keep the auto-apply-on-boot ergonomic.

Exit codes:
  0  no pending migrations OR all pending migrations applied
  1  apply failed (wrapped store error message in stderr)

Examples:
  # K8s InitContainer (recommended pre-§4 closure)
  - name: migrate
    image: hermem:latest
    args: ["db", "migrate", "apply"]

  # One-shot operator invocation
  hermem db migrate apply`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := migration.New(env.DB)
			// Pre-apply DryRun is the "nothing-to-do" early-out: if
			// DryRun returns zero pending we short-circuit before any
			// apply SQL runs, so a freshly-bootstrapped DB is a no-op.
			pre, err := svc.DryRun(env.Ctx)
			if err != nil {
				return fmt.Errorf("migrate apply (pre-check): %w", err)
			}
			if len(pre) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Already up-to-date.")
				return nil
			}
			// Anchor the headline "applied N" against the PRE-snapshot
			// name set so a partial mid-apply failure cannot inflate
			// the count — post-Status returns rows whose WAS-applied
			// status was already true on the pre-snapshot AND whose
			// status is now true. The diff between pre-applied and
			// post-applied can never exceed len(pre), and a partial
			// failure shows a smaller delta than the caller expected
			// (operator-correct: prefer under-reporting over silent
			// over-reporting).
			prePendingNames := make(map[string]bool, len(pre))
			for _, m := range pre {
				prePendingNames[m.Name] = true
			}
			post, err := svc.Run(env.Ctx)
			if err != nil {
				return fmt.Errorf("migrate apply: %w", err)
			}
			// Build the applied-set from the post-Status. Filter to
			// migrations that were pending on the pre-snapshot (so
			// already-applied migrations don't inflate the count if
			// a future refactor makes `Run` permissive about pristine
			// state).
			var applied []store.MigStatus
			for _, m := range post {
				if !m.Applied {
					continue
				}
				if prePendingNames[m.Name] {
					applied = append(applied, m)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Applied %d migration(s):\n", len(applied))
			for _, m := range applied {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s", m.Name)
				if m.ChecksumSHA256 != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  sha256:%s", m.ChecksumSHA256[:12])
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
}

// runMigrateStatus is the shared body for `hermem db migrate` and
// `hermem db migrate status`. PHASE 3.2 routes through the
// transport-agnostic migration Service rather than hitting store.*
// directly.
func runMigrateStatus(cmd *cobra.Command, env *cli.Env) error {
	svc := migration.New(env.DB)
	status, err := svc.Status(env.Ctx)
	if err != nil {
		return fmt.Errorf("migrate status: %w", err)
	}
	for _, m := range status {
		mark := "--"
		if m.Applied {
			mark = "OK"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s", mark, m.Name)
		if m.AppliedAt != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  (%s)", m.AppliedAt)
		}
		if m.ChecksumSHA256 != "" {
			match := "ok"
			if m.ChecksumMatch != nil && !*m.ChecksumMatch {
				match = "MISMATCH"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  sha256:%s [%s]", m.ChecksumSHA256[:12], match)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}
