package cmd

import (
	"fmt"
	"log"
	"strconv"

	"github.com/pavelveter/hermem/src/internal/algo"
)

func init() { Register("re-embed", cliReEmbed) }

func cliReEmbed(env Env) {
	batchSize := 50
	model := ""
	args := argTail()
	for i := 0; i < len(args); i++ {
		if args[i] == "--batch-size" {
			batchSize, _ = strconv.Atoi(args[i+1])
			i++
		}
		if args[i] == "--model" {
			model = args[i+1]
			i++
		}
	}
	result, err := algo.ReEmbedAll(env.Ctx, env.DB, env.VI, env.Embedder, env.Cfg.VectorDim, batchSize, model)
	if err != nil {
		log.Fatalf("re-embed: %v", err)
	}
	fmt.Printf("Re-embed: %d/%d entities (failed=%d, batches=%d, elapsed=%s)\n",
		result.ReEmbedded, result.TotalEntities, result.Failed, result.Batches, result.Elapsed)
}
