package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// New source modules use the indexed descriptor ABI and the complete execution
// contract. Operation-specific markers remain decoder compatibility metadata.
func stagedNativeTable64ABI(*wasm.Module) uint8 {
	return nativeTable64NoABI
}
