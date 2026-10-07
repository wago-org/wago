//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo && wago_precompiled

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type railshotCompiledModule = encoder.CompiledModule

func railshotAdmitSource([]byte, *wasm.Module) error {
	return fmt.Errorf("source compilation is unavailable in a precompiled runtime")
}

func railshotCompileModuleWith(*wasm.Module, railshotCompileOptions) (*railshotCompiledModule, error) {
	return nil, fmt.Errorf("source compilation is unavailable in a precompiled runtime")
}

func railshotHostIndirectThunk(uint32) []byte { return nil }

func railshotHostIndirectSyncThunk(index uint32, params, results int) []byte {
	return railshot.HostIndirectSyncThunk(index, params, results)
}

func railshotHostIndirectOwnedSyncThunk(index uint32, params, results int) []byte {
	return railshot.HostIndirectOwnedSyncThunk(index, params, results)
}
