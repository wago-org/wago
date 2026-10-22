//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func threadedGrowthHelpers(*wasm.Module) bool { return false }
func threadedMemorySizeGrowSupported() bool   { return false }
func (*Instance) dispatchAtomicMemoryGrow([]uint64) (uint32, error) {
	return 0, fmt.Errorf("shared memory growth helper is unavailable")
}
