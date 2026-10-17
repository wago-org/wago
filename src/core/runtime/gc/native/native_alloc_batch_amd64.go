package gc

import "os"

const nativeStructHandleBatch = 512

var nativeStructHandleBatchSize = func() int {
	switch os.Getenv("WAGO_AMD64_GC_STRUCT_BATCH") {
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
