//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Optional compilation scratch must not grow without a fixed byte budget.
const maxModuleTypeCacheBytes = 1 << 20

// moduleTypeCache avoids repeatedly walking imports to resolve indexed memories
// and globals while compiling large modules. Tiny modules keep direct lookups so
// the cache never adds allocations to their fast path.
type moduleTypeCache struct {
	funcCount       int
	funcStart       int
	memories        []wasm.MemType
	globals         []wasm.GlobalType
	funcsContiguous bool
	valid           bool
}
