// Package agent hosts the agentic-flow commands.
//
//	hermem agent loop      # run algo.AgentLoop on a goal_id
package agent

import (
	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// NewCmd returns the agent group cobra command.
func NewCmd(env cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agentic flows (loop)",
	}
	cmd.AddCommand(newLoopCmd(env))
	return cmd
}
