//go:build arm64

package arm64

import "os"

var selectSinkPureGuardEnabled = os.Getenv("WAGO_ARM64_NO_SELECT_SINK_PURE_GUARD") != "1"
