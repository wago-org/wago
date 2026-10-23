//go:build arm64

package arm64

import "os"

var brTableBranchVectorEnabled = os.Getenv("WAGO_ARM64_NO_BR_TABLE_BRANCH_VECTOR") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_BR_TABLE_BRANCH_VECTOR") != "0"
