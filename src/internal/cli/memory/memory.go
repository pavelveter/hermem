// Package memory hosts the knowledge-CRUED + retrieval commands.
//
//	hermem memory <sub>          # store, search, retrieve, query, response,
//	                             # edge, ingest, explain, re-embed, quantize
//
// All subcommands consume JSON from stdin via cli.DecodeStdin; see each
// subcommand file for the request shape.
package memory

import (
	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
)

// NewCmd returns the memory group cobra command. Attach it under the root
// to expose `hermem memory <sub>`.
func NewCmd(env *cli.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Knowledge CRUD and retrieval (store / search / retrieve / query / response / edge / ingest / explain / re-embed / quantize)",
		Long: `Manage the knowledge graph: store entities, search by semantic similarity,
retrieve related facts, query with LLM context, manage edges between entities,
ingest dialog transcripts, explain retrieval decisions, re-embed after model
changes, and quantize embeddings.

All subcommands read JSON from stdin. Use "hermem memory <sub> --help" for
request schema and examples.`,
	}
	cmd.AddCommand(
		newStoreCmd(env),
		newSearchCmd(env),
		newRetrieveCmd(env),
		newQueryCmd(env),
		newResponseCmd(env),
		newEdgeCmd(env),
		newIngestCmd(env),
		newExplainCmd(env),
		newReEmbedCmd(env),
		newQuantizeCmd(env),
	)
	return cmd
}
