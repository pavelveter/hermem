package cmd

import (
	"fmt"
	"log"

	"github.com/pavelveter/hermem/src/internal/store"
)

func init() { Register("schema", cliSchema) }

func cliSchema(env Env) {
	stored, current, err := store.CheckSchemaFingerprint(env.DB, env.Cfg.Schema)
	if err != nil {
		log.Fatalf("schema: %v", err)
	}
	fmt.Printf("Current: %s\nStored:   %s\n", current, stored)
	if stored != "" && stored != current {
		fmt.Println("WARNING: schema changed!")
	}
}
