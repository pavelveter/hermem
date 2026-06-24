package cmd

import (
	"fmt"
	"os"
)

func init() { Register("health", cliHealth) }

func cliHealth(env Env) {
	if err := env.DB.PingContext(env.Ctx); err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		os.Exit(1)
	}
	_ = writeJSON(os.Stdout, map[string]any{
		"status": "ok",
		"checks": map[string]string{"database": "ok"},
	})
}
