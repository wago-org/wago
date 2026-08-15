//go:build arm64 && wago_precompiled

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/hostthunk"
)

type railshotCompiledModule = encoder.CompiledModule

func railshotCompileModuleWith(*wasm.Module, railshotCompileOptions) (*railshotCompiledModule, error) {
	return nil, fmt.Errorf("source compilation is unavailable in a precompiled runtime")
}

func railshotHostIndirectThunk(importIdx uint32) []byte {
	return hostthunk.Indirect(importIdx)
}

func railshotHostIndirectSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return hostthunk.IndirectSync(importIdx, paramSlots, resultSlots)
}

func railshotHostIndirectOwnedSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return hostthunk.IndirectOwnedSync(importIdx, paramSlots, resultSlots)
}
