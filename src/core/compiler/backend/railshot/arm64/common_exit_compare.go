//go:build arm64

package arm64

import "os"

var commonExitCompareEnabled = os.Getenv("WAGO_ARM64_NO_COMMON_EXIT_COMPARE") != "1" &&
	os.Getenv("WAGO_ARM64_EXPERIMENT_COMMON_EXIT_COMPARE") != "0"
