//go:build arm64

package arm64

import "os"

var dominatedIndexedBaseEnabled = os.Getenv("WAGO_ARM64_NO_DOMINATED_INDEXED_BASE") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE") != "0"
