//go:build arm64

package arm64

import "os"

var ordinaryShiftedImmediateEnabled = os.Getenv("WAGO_ARM64_NO_ORDINARY_SHIFTED_IMMEDIATE") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE") != "0"
