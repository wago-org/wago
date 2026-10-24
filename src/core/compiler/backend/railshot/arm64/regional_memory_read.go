//go:build arm64

package arm64

import "os"

var regionalMemoryReadEnabled = os.Getenv("WAGO_ARM64_NO_REGIONAL_MEMORY_READ") != "1" && os.Getenv("WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ") != "0"
