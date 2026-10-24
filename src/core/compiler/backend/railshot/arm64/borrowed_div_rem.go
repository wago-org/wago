//go:build arm64

package arm64

import "os"

var borrowedDivRemEnabled = os.Getenv("WAGO_ARM64_NO_BORROWED_DIV_REM") != "1" &&
	os.Getenv("WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM") != "0"
