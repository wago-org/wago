//go:build amd64

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

type railshotImportBinding = railshot.ImportBinding
type railshotCompileOptions = railshot.CompileOptions
type railshotCompiledModule = encoderamd64.CompiledModule

func railshotCompileModuleWith(m *wasm.Module, opts railshotCompileOptions) (*railshotCompiledModule, error) {
	return railshot.CompileModuleWith(m, opts)
}

func railshotHostIndirectThunk(importIdx uint32) []byte {
	return railshot.HostIndirectThunk(importIdx)
}

func railshotHostIndirectSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return railshot.HostIndirectSyncThunk(importIdx, paramSlots, resultSlots)
}

func railshotHostIndirectOwnedSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return railshot.HostIndirectOwnedSyncThunk(importIdx, paramSlots, resultSlots)
}
