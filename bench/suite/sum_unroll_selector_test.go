//go:build amd64 && wago_sumunroll

package wagobench

import (
	"os"
	_ "unsafe" // Test-only access to the isolated compiler experiment.
)

// The private scalar-argument setter avoids sharing a record layout across
// packages. The bridge exists only in corpus test binaries, outside all timers.
//
//go:linkname setSumUnrollMeasurement github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64.setSumUnrollMeasurement
func setSumUnrollMeasurement(factor int, hybrid bool)

func init() {
	switch v := os.Getenv("WAGO_SUM_VARIANT"); v {
	case "", "baseline", "default":
	case "D", "H":
		setSumUnrollMeasurement(16, v == "H")
	default:
		panic("unknown corpus sum variant: " + v)
	}
}
