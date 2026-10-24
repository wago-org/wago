//go:build arm64

package arm64

import (
	"os"
)

var boundedSignedQuotientEnabled = os.Getenv("WAGO_ARM64_NO_BOUNDED_SIGNED_QUOTIENT") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_BOUNDED_SIGNED_QUOTIENT") != "0"
