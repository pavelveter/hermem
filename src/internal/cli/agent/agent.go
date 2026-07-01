// Package agent hosts the agentic-flow commands.
//
//	hermem agent loop      # run algo.AgentLoop on a goal_id
package agent

import (
	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// NewCmd returns the agent group cobra command.
func NewCmd(env *cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agentic flows (loop)",
		Long: `Run autonomous agent loops that plan, claim, execute, and persist tasks
using the LLM extraction pipeline. The agent loop processes a goal by
creating tasks, executing them via the AI pipeline, and tracking progress.

Use "hermem agent loop --help" for the goal request schema.`,
	}
	cmd.AddCommand(newLoopCmd(env))
	return cmd
}
