// Package graph hosts the graph algorithm + analytics commands.
//
//	hermem graph <sub>    # plan / recovery-plan / components / communities /
//	                     # verify / contradictions / provenance
//
// `contradictions` takes an optional [entity-id] positional arg; the rest
// are JSON-stdin driven. `provenance` uses real cobra flags
// (--conversation / --message / --source / --limit).
package graph

import (
	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// NewCmd returns the graph group cobra command. Attach it under the root
// to expose `hermem graph <sub>`.
func NewCmd(env cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Graph algorithms (plan / recovery-plan / components / communities / verify / contradictions / provenance)",
	}
	cmd.AddCommand(
		newPlanCmd(env),
		newRecoveryPlanCmd(env),
		newComponentsCmd(env),
		newCommunitiesCmd(env),
		newVerifyCmd(env),
		newContradictionsCmd(env),
		newProvenanceCmd(env),
	)
	return cmd
}
