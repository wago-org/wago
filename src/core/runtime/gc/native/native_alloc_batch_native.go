//go:build amd64 || arm64

package gc

import (
	"os"
	"runtime"
	"strings"
)

const nativeStructHandleBatch = 512

var nativeStructHandleBatchSize = func() int {
	switch os.Getenv("WAGO_" + strings.ToUpper(runtime.GOARCH) + "_GC_STRUCT_BATCH") {
	case "32":
		return 32
	case "64":
		return 64
	case "128":
		return 128
	case "256":
		return 256
	default:
		return nativeStructHandleBatch
	}
}()
