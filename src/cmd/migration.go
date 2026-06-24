package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/store"
)

func init() {
	Register("migrate", cliMigrate)
	Register("migration-rollback", cliMigrationRollback)
	Register("migration-verify", cliMigrationVerify)
}

func cliMigrate(env Env) {
	status, err := store.MigrationStatus(env.DB)
	if err != nil {
		log.Fatalf("migrate status: %v", err)
	}
	for _, m := range status {
		mark := "  "
		if m.Applied {
			mark = "OK"
		} else {
			mark = "--"
		}
		fmt.Printf("[%s] %s", mark, m.Name)
		if m.AppliedAt != "" {
			fmt.Printf("  (%s)", m.AppliedAt)
		}
		fmt.Println()
	}
}

func cliMigrationRollback(env Env) {
	name, err := store.RollbackMigration(env.DB)
	if err != nil {
		log.Fatalf("rollback: %v", err)
	}
	if name == "" {
		fmt.Println("No migrations.")
	} else {
		fmt.Printf("Rolled back: %s\n", name)
	}
}

func cliMigrationVerify(env Env) {
	mismatches, err := store.VerifyMigrationIntegrity(env.DB)
	if err != nil {
		log.Fatalf("verify: %v", err)
	}
	if len(mismatches) == 0 {
		fmt.Println("All migration checksums intact.")
	} else {
		fmt.Printf("%d mismatch(es)\n", len(mismatches))
		os.Exit(1)
	}
}
