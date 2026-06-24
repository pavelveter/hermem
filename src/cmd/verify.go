package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/algo"
)

func init() { Register("verify", cliVerify) }

func cliVerify(env Env) {
	report, err := algo.VerifyGraph(env.DB, env.Cfg.Schema, env.Cfg.VectorDim)
	if err != nil {
		log.Fatalf("verify: %v", err)
	}
	fmt.Print(report.String())
	if !report.Pass() {
		os.Exit(1)
	}
}
