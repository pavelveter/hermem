package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
)

// newProvenanceCmd queries entities by provenance triple. The
// domain Service.Provenance wraps store.GetEntitiesByProvenance and
// defaults limit via retrieval.DefaultProvenanceLimit when <= 0.
func newProvenanceCmd(env *cli.Env) *cobra.Command {
	var (
		convID string
		msgID  string
		source string
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "provenance",
		Short: "Query entities by provenance (conversation / message / source)",
		Long: `Query entities by their provenance metadata (origin information).

Every entity can optionally record where it came from: which conversation,
which message, or which source. This command finds entities matching
the given provenance filters.

Flags:
  --conversation    Filter by conversation ID
  --message         Filter by message ID
  --source          Filter by source string
  --limit           Max entities returned (default 50)

At least one filter should be provided for meaningful results.

Output (text, one entity per line):
  [entity-id] category  content  conv=conv-id msg=msg-id

Examples:
  hermem graph provenance --conversation conv-1
  hermem graph provenance --source "manual-import" --limit 10
  hermem graph provenance --message msg-42`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := retdomain.New(env.DB, env.VI, env.Embedder)
			entities, err := svc.Provenance(env.Ctx, convID, msgID, source, limit)
			if err != nil {
				return fmt.Errorf("provenance: %w", err)
			}
			for _, e := range entities {
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s  [%s]  conv=%s msg=%s\n",
					e.ID, e.Category, e.Content, e.ConversationID, e.MessageID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&convID, "conversation", "", "filter by conversation id")
	cmd.Flags().StringVar(&msgID, "message", "", "filter by message id")
	cmd.Flags().StringVar(&source, "source", "", "filter by source")
	cmd.Flags().IntVar(&limit, "limit", 50, "max entities returned")
	return cmd
}
