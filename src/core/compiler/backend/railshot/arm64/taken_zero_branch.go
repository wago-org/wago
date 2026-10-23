//go:build arm64

package arm64

import "os"

var takenZeroBranchEnabled = os.Getenv("WAGO_ARM64_NO_TAKEN_ZERO_BRANCH") != "1"
