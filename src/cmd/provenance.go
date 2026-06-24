package cmd

import (
	"fmt"
	"log"
	"strconv"

	"github.com/pavelveter/hermem/src/internal/store"
)

func init() { Register("provenance", cliProvenance) }

func cliProvenance(env Env) {
	args := argTail()
	var convID, msgID, source string
	limit := 50
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--conversation":
			convID = args[i+1]
			i++
		case "--message":
			msgID = args[i+1]
			i++
		case "--source":
			source = args[i+1]
			i++
		case "--limit":
			limit, _ = strconv.Atoi(args[i+1])
			i++
		}
	}
	entities, err := store.GetEntitiesByProvenance(env.DB, convID, msgID, source, limit)
	if err != nil {
		log.Fatalf("provenance: %v", err)
	}
	for _, e := range entities {
		fmt.Printf("[%s] %s  [%s]  conv=%s msg=%s\n", e.ID, e.Category, e.Content, e.ConversationID, e.MessageID)
	}
}
