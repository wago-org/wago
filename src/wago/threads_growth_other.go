//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"fmt"
)

func threadedMemorySizeGrowSupported() bool { return false }
func (*Instance) dispatchAtomicMemoryGrow([]uint64) (uint32, error) {
	return 0, fmt.Errorf("shared memory growth helper is unavailable")
}
