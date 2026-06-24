package cmd

import (
	"fmt"
	"log"

	"github.com/pavelveter/hermem/src/internal/vector"
)

func init() { Register("quantize", cliQuantize) }

func cliQuantize(_ Env) {
	var req struct {
		Embedding []float32 `json:"embedding"`
	}
	DecodeStdin(&req)
	if len(req.Embedding) == 0 {
		log.Fatal("embedding required")
	}
	qv := vector.QuantizeVector(req.Embedding)
	deq := vector.DequantizeVector(qv)
	fmt.Printf("Original: %d elements (%d bytes)\n", len(req.Embedding), len(req.Embedding)*4)
	fmt.Printf("Quantized: %d bytes (%.1fx)\n", 8+len(qv.Codes), float64(len(req.Embedding)*4)/float64(8+len(qv.Codes)))
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
	fmt.Printf("Max error: %.6f\n", maxErr)
}
