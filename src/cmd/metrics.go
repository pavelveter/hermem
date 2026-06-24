package cmd

import (
	"os"

	"github.com/pavelveter/hermem/src/internal/metrics"
)

func init() { Register("metrics", cliMetrics) }

func cliMetrics(_ Env) {
	metrics.WriteExposition(os.Stdout)
}
