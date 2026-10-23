//go:build arm64

package arm64

import (
	"os"
)

var csetLineageEnabled = os.Getenv("WAGO_ARM64_NO_CSET_LINEAGE") != "1"
