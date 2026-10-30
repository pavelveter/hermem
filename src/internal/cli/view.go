package cli

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	viewdomain "github.com/pavelveter/hermem/src/internal/view"
)

// newViewCmd boots the loopback graph viewer. It lives at the CLI root
// (not under `hermem graph`) because it is an interactive process, not a
// one-shot analysis command.
func newViewCmd(env *clienv.Env) *cobra.Command {
	var port string
	var noOpen bool
	var limit int

	cmd := &cobra.Command{
		Use:   "view",
		Short: "Open the knowledge graph in a browser (local, read-only)",
		Long: `Serve an interactive graph of the active database on 127.0.0.1.

The page renders entities as nodes (colour = category, radius = degree) and
typed edges between them. Drag a node to pin it, drag the background to pan,
scroll to zoom, click a node for its content and connections, and toggle node
types / relationships in the sidebar. Refresh re-reads the database.

The listener binds loopback only and never writes. It blocks until Ctrl-C.

The snapshot is capped at ` + strconv.Itoa(viewdomain.NodeCap) + ` nodes (highest degree first) to keep the
force simulation interactive; use "hermem graph components" / "communities"
for full-database analysis.

Flags:
  --port, -p     TCP port on 127.0.0.1 (default 8421)
  --no-open      Do not launch a browser automatically`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			err := viewdomain.ServeAndWait(env.Ctx, env.DB, port, limit, func(url string) {
				fmt.Fprintf(out, "hermem view: %s\n", url)
				if noOpen {
					return
				}
				if err := openBrowser(url); err != nil {
					// A headless box is not an error — the URL is printed.
					slog.Warn("view: could not open browser", "err", err, "url", url)
				}
			})
			if err != nil {
				// ctx cancellation is a normal Ctrl-C shutdown.
				if env.Ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("view: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&port, "port", "p", "8421", "TCP port to listen on (loopback only)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not launch a browser")
	cmd.Flags().IntVar(&limit, "limit", viewdomain.NodeCap,
		fmt.Sprintf("Maximum nodes to render (capped at %d)", viewdomain.NodeCap))
	return cmd
}

// openBrowser launches the platform opener. Detached from the process so
// closing hermem view does not close the tab.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
