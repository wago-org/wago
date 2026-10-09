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
func setSumUnrollMeasurement(factor int, hybrid bool, threshold int)

func init() {
	switch v := os.Getenv("WAGO_SUM_VARIANT"); v {
	case "", "baseline", "default":
	case "D", "H", "T64", "T128", "T256":
		threshold := 0
		switch v {
		case "T64":
			threshold = 64
		case "T128":
			threshold = 128
		case "T256":
			threshold = 256
		}
		setSumUnrollMeasurement(16, v == "H", threshold)
	default:
		panic("unknown corpus sum variant: " + v)
	}
}
