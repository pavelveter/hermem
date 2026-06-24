package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/store"
)

func init() { Register("contradictions", cliContradictions) }

func cliContradictions(env Env) {
	id := ""
	if len(os.Args) > 2 {
		id = os.Args[2]
	}
	pairs, err := store.GetContradictions(env.DB, id)
	if err != nil {
		log.Fatalf("contradictions: %v", err)
	}
	for _, p := range pairs {
		fmt.Printf("[%s] %s\n  contradicts [%s] %s\n\n", p.SourceID, p.SourceContent, p.TargetID, p.TargetContent)
	}
}
