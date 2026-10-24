//go:build arm64

package arm64

import "os"

var selectSourceReadEnabled = os.Getenv("WAGO_ARM64_NO_SELECT_SOURCE_READ") != "1"
