//go:build linux && riscv64

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

// detectRISCV64SIMDHostFeatures is retained as the architecture-local RVV tier
// hook. Baseline WebAssembly SIMD uses RV64G SWAR regardless of this result.
func detectRISCV64SIMDHostFeatures() bool { return coreruntime.RISCV64HasRVV() }
