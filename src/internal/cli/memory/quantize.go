package memory

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func newQuantizeCmd(env *cli.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "quantize",
		Short: "Quantize a single embedding locally (no DB / no embedder call)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var req struct {
				Embedding []float32 `json:"embedding"`
			}
			if err := cli.DecodeStdin(&req); err != nil {
				return err
			}
			if len(req.Embedding) == 0 {
				return fmt.Errorf("embedding required")
			}
			qv := vector.QuantizeVector(req.Embedding)
			deq := vector.DequantizeVector(qv)
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Original: %d elements (%d bytes)\n", len(req.Embedding), len(req.Embedding)*4)
			fmt.Fprintf(out, "Quantized: %d bytes (%.1fx)\n", 8+len(qv.Codes), float64(len(req.Embedding)*4)/float64(8+len(qv.Codes)))
			var maxErr float32
			for i := range req.Embedding {
				e := req.Embedding[i] - deq[i]
				if e < 0 {
					e = -e
				}
				if e > maxErr {
					maxErr = e
				}
			}
			fmt.Fprintf(out, "Max error: %.6f\n", maxErr)
			return nil
		},
	}
}
