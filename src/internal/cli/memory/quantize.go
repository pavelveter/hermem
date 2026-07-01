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
		Long: `Quantize a float32 embedding vector into a compact binary representation.

Input (JSON on stdin):
  {
    "embedding": [0.1, -0.3, 0.5, ...]
  }

This is a local, stateless operation — no database access, no embedder
call. It uses product quantization to compress the embedding vector,
reporting:
  - Original size (elements × 4 bytes)
  - Quantized size (8-byte header + codes)
  - Compression ratio
  - Maximum reconstruction error

Useful for estimating storage savings before enabling quantization
on the vector index, or for testing quantization quality on sample
vectors.

Output (text):
  Original: 768 elements (3072 bytes)
  Quantized: 96 bytes (32.0x)
  Max error: 0.012345

Examples:
  echo '{"embedding":[0.1,0.2,0.3]}' | hermem memory quantize
  echo '{"embedding":[0.1,0.2,0.3]}' | hermem memory quantize | grep "Max error"`,
		Args: cobra.NoArgs,
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
