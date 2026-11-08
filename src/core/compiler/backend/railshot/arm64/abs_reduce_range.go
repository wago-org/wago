//go:build arm64

package arm64

import "os"

var absReduceRangeEnabled = os.Getenv("WAGO_ARM64_NO_ABS_REDUCE_RANGE") != "1"
