//go:build amd64 && wago_sumunroll

package wagobench

import (
	"os"
	_ "unsafe" // Test-only access to the isolated compiler experiment.
)

// Match the private, tagged experiment record. This bridge is linked only into
// benchmark/test binaries; it adds no compiler API or normal-build operation.
//
//go:linkname sumUnrollSelection github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64.sumUnrollExperiment
var sumUnrollSelection struct{ factor, chains, budget int }

func init() {
	switch v := os.Getenv("WAGO_SUM_VARIANT"); v {
	case "", "baseline", "default":
	case "D":
		sumUnrollSelection.factor, sumUnrollSelection.chains = 16, 4
		sumUnrollSelection.budget = 512
	default:
		panic("unknown corpus sum variant: " + v)
	}
}
