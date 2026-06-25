// Package profile hosts ad-hoc profiling subcommands for the running
// hermem process. Each subcommand does NOT require a live daemon — it
// spawns a short-lived embedded pprof worker that captures the requested
// profile locally using the current process's runtime state.
//
//	hermem profile cpu <duration>      # CPU profile (seconds)
//	hermem profile heap                # heap snapshot -> /tmp/hermem-heap.pprof
//	hermem profile goroutine           # goroutine dump -> stdout
//	hermem profile trace <duration>    # execution trace -> /tmp/hermem-trace.out
package profile

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
)

const defaultDuration = 10 * time.Second

// heapOutputPath / traceOutputPath are the default destinations for
// the heap + trace subcommands. Centralised so the path is greppable
// from one location and a future move to hermem.ini stays one diff.
const (
	heapOutputPath  = "/tmp/hermem-heap.pprof"
	traceOutputPath = "/tmp/hermem-trace.out"
)

// NewCmd returns the profile group cobra command.
func NewCmd(env *clienv.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Ad-hoc profiling: cpu / heap / goroutine / trace",
		Long: "Capture a runtime profile from the hermem process without a live " +
			"server. Each subcommand runs in-process and writes its result to " +
			"stdout or a well-known /tmp path.",
	}
	cmd.AddCommand(
		newCPUCmd(),
		newHeapCmd(),
		newGoroutineCmd(),
		newTraceCmd(),
	)
	_ = env // profile commands are env-independent (no DB, no embedder)
	return cmd
}

// newCPUCmd captures a CPU profile of duration seconds (default 10).
// Writes raw protobuf to stdout — pipe to a .pprof file or `go tool pprof`.
func newCPUCmd() *cobra.Command {
	var secs int
	cmd := &cobra.Command{
		Use:   "cpu [duration]",
		Short: "CPU profile (seconds, default 10) — protobuf to stdout",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := defaultDuration
			if len(args) == 1 {
				parsed, err := time.ParseDuration(args[0] + "s")
				if err != nil {
					return fmt.Errorf("invalid duration %q: %w", args[0], err)
				}
				d = parsed
			} else if secs > 0 {
				d = time.Duration(secs) * time.Second
			}
			return captureCPUProfile(cmd.OutOrStdout(), d)
		},
	}
	cmd.Flags().IntVarP(&secs, "seconds", "s", 0, "override duration in seconds")
	return cmd
}

// captureCPUProfile starts the global CPU profiler, sleeps for d, then
// stops it. Stdout receives the protobuf bytes.
func captureCPUProfile(w io.Writer, d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("duration must be positive (got %s)", d)
	}
	if err := pprof.StartCPUProfile(w); err != nil {
		return fmt.Errorf("start cpu profile: %w", err)
	}
	time.Sleep(d)
	pprof.StopCPUProfile()
	return nil
}

// newHeapCmd writes the current heap profile to /tmp/hermem-heap.pprof.
// The path is fixed (not stdout) so a downstream `go tool pprof` can
// load the file directly without juggling pipes.
func newHeapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "heap",
		Short: "Heap snapshot -> /tmp/hermem-heap.pprof",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			f, err := os.Create(heapOutputPath)
			if err != nil {
				return fmt.Errorf("create heap dump: %w", err)
			}
			defer f.Close()
			// GC before sampling so the heap profile reflects live
			// reachable state rather than transient allocations from
			// the GC itself.
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				return fmt.Errorf("write heap profile: %w", err)
			}
			fmt.Fprintf(os.Stderr, "heap profile written: %s\n", heapOutputPath)
			return nil
		},
	}
}

// newGoroutineCmd dumps the live goroutine stacks to stdout (text
// format). The output is consumable by `go tool pprof -text` or
// simply inspected by eye for stuck goroutines.
func newGoroutineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "goroutine",
		Short: "Goroutine dump -> stdout",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return pprof.Lookup("goroutine").WriteTo(cmd.OutOrStdout(), 1)
		},
	}
}

// newTraceCmd captures an execution trace for duration seconds (default
// 10) into /tmp/hermem-trace.out. Analyze with `go tool trace <path>`.
func newTraceCmd() *cobra.Command {
	var secs int
	cmd := &cobra.Command{
		Use:   "trace [duration]",
		Short: "Execution trace (seconds, default 10) -> /tmp/hermem-trace.out",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			d := defaultDuration
			if len(args) == 1 {
				parsed, err := time.ParseDuration(args[0] + "s")
				if err != nil {
					return fmt.Errorf("invalid duration %q: %w", args[0], err)
				}
				d = parsed
			} else if secs > 0 {
				d = time.Duration(secs) * time.Second
			}
			return captureTrace(d)
		},
	}
	cmd.Flags().IntVarP(&secs, "seconds", "s", 0, "override duration in seconds")
	return cmd
}

// captureTrace runs the global execution tracer for d then writes the
// captured trace to traceOutputPath. The trace is opaque binary — open
// with `go tool trace`.
func captureTrace(d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("duration must be positive (got %s)", d)
	}
	f, err := os.Create(traceOutputPath)
	if err != nil {
		return fmt.Errorf("create trace output: %w", err)
	}
	defer f.Close()
	if err := trace.Start(f); err != nil {
		return fmt.Errorf("start trace: %w", err)
	}
	time.Sleep(d)
	trace.Stop()
	fmt.Fprintf(os.Stderr, "trace written: %s\n", traceOutputPath)
	return nil
}
