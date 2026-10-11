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

func railshotI31CoreSource([]byte) bool                                     { return false }
func railshotConfigureI31Core(bool, *railshotCompileOptions)                {}
func railshotGCRefEqSource([]byte) bool                                     { return false }
func railshotConfigureGCRefEq(bool, *railshotCompileOptions)                {}
func railshotGCStructGetSource([]byte) bool                                 { return false }
func railshotConfigureGCStructGet(bool, *railshotCompileOptions)            {}
func railshotGCStructNullSource([]byte) bool                                { return false }
func railshotConfigureGCStructNull(bool, *railshotCompileOptions)           {}
func railshotGCStructPackedSource([]byte) bool                              { return false }
func railshotConfigureGCStructPacked(bool, *railshotCompileOptions)         {}
func railshotGCStructBasicSource([]byte) bool                               { return false }
func railshotConfigureGCStructBasic(bool, *railshotCompileOptions)          {}
func railshotGCArrayLocalSource([]byte) bool                                { return false }
func railshotConfigureGCArrayLocal(bool, *railshotCompileOptions)           {}
func railshotGCArrayUniformSource([]byte) bool                              { return false }
func railshotConfigureGCArrayUniform(bool, *railshotCompileOptions)         {}
func railshotGCArrayFixedLocalSource([]byte) bool                           { return false }
func railshotConfigureGCArrayFixedLocal(bool, *railshotCompileOptions)      {}
func railshotGCArrayFixedOfficialSource([]byte) bool                        { return false }
func railshotConfigureGCArrayFixedOfficial(bool, *railshotCompileOptions)   {}
func railshotGCArrayDefaultOfficialSource([]byte) bool                      { return false }
func railshotConfigureGCArrayDefaultOfficial(bool, *railshotCompileOptions) {}
func railshotGCArrayPackedDataSource([]byte) bool                           { return false }
func railshotConfigureGCArrayPackedData(bool, *railshotCompileOptions)      {}
func railshotGCArrayReferenceSource([]byte) bool                            { return false }
func railshotConfigureGCArrayReference(bool, *railshotCompileOptions)       {}
func railshotGCArrayInitElemSource([]byte) bool                             { return false }
func railshotConfigureGCArrayInitElem(bool, *railshotCompileOptions)        {}
func railshotGCArrayInitDataSource([]byte) bool                             { return false }
func railshotConfigureGCArrayInitData(bool, *railshotCompileOptions)        {}
func railshotGCArrayFillSource([]byte) bool                                 { return false }
func railshotConfigureGCArrayFill(bool, *railshotCompileOptions)            {}
func railshotGCArrayCopySource([]byte) bool                                 { return false }
func railshotConfigureGCArrayCopy(bool, *railshotCompileOptions)            {}

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
