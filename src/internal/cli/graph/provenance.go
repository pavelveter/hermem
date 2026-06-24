package graph

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/store"
)

// newProvenanceCmd — pre-cobra parsed flags from os.Args[2:]. Now real
// cobra flags with --help strings.
func newProvenanceCmd(env cli.Env) *cobra.Command {
	var (
		convID string
		msgID  string
		source string
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "provenance",
		Short: "Query entities by provenance (conversation / message / source)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit <= 0 {
				limit = 50
			}
			entities, err := store.GetEntitiesByProvenance(env.DB, convID, msgID, source, limit)
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
