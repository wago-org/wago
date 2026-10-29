//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && wago_standalone

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type railshotCompiledModule = encoder.CompiledModule

func railshotAdmitSource([]byte, *wasm.Module) error {
	return fmt.Errorf("source compilation is unavailable in a standalone executable")
}

func railshotI31CoreSource([]byte) bool                                     { return false }
func railshotConfigureI31Core(bool, *railshotCompileOptions)                {}
func railshotGeneralI31Module(*wasm.Module) bool                            { return false }
func railshotConfigureGeneralI31(bool, *railshotCompileOptions)             {}
func railshotGCRefEqSource([]byte) bool                                     { return false }
func railshotConfigureGCRefEq(bool, *railshotCompileOptions)                {}
func railshotGCRefTestTableSource([]byte) bool                              { return false }
func railshotConfigureGCRefTestTable(bool, *railshotCompileOptions)         {}
func railshotGCRefTestConcreteSource([]byte) bool                           { return false }
func railshotConfigureGCRefTestConcrete(bool, *railshotCompileOptions)      {}
func railshotGCRefTestAbstractSource([]byte) bool                           { return false }
func railshotConfigureGCRefTestAbstract(bool, *railshotCompileOptions)      {}
func railshotGCExternSource([]byte) bool                                    { return false }
func railshotConfigureGCExtern(bool, *railshotCompileOptions)               {}
func railshotGCRefCastAbstractSource([]byte) bool                           { return false }
func railshotConfigureGCRefCastAbstract(bool, *railshotCompileOptions)      {}
func railshotGCRefCastConcreteSource([]byte) bool                           { return false }
func railshotConfigureGCRefCastConcrete(bool, *railshotCompileOptions)      {}
func railshotGCBrOnCastAbstractSource([]byte) bool                          { return false }
func railshotConfigureGCBrOnCastAbstract(bool, *railshotCompileOptions)     {}
func railshotGCBrOnCastFailAbstractSource([]byte) bool                      { return false }
func railshotConfigureGCBrOnCastFailAbstract(bool, *railshotCompileOptions) {}
func railshotGCBrOnCastConcreteSource([]byte) bool                          { return false }
func railshotConfigureGCBrOnCastConcrete(bool, *railshotCompileOptions)     {}
func railshotGCBrOnCastFailConcreteSource([]byte) bool                      { return false }
func railshotConfigureGCBrOnCastFailConcrete(bool, *railshotCompileOptions) {}
func railshotGCBrOnCastNullabilitySource([]byte) bool                       { return false }
func railshotConfigureGCBrOnCastNullability(bool, *railshotCompileOptions)  {}
func railshotGCBrOnCastFailNullabilitySource([]byte) bool                   { return false }
func railshotConfigureGCBrOnCastFailNullability(bool, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingDeclarationsSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingDeclarations(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingRecursiveFunctionsSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingRecursiveFunctions(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingRefFuncGlobalsSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingRefFuncGlobals(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingRuntimeCallCastSource([]byte) bool { return false }
func railshotConfigureGCTypeSubtypingRuntimeCallCast(bool, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingRuntimeFinalityCallCastSource([]byte) bool { return false }
func railshotConfigureGCTypeSubtypingRuntimeFinalityCallCast(bool, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingRuntimeTypedTableCallSource([]byte) bool { return false }
func railshotConfigureGCTypeSubtypingRuntimeTypedTableCall(bool, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingRefTestSingleSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingRefTestSingle(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingFinalityLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingFinalityLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingStructLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingStructLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingStructProjectionLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingStructProjectionLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingStructMismatchLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingStructMismatchLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingIndependentStructLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingIndependentStructLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingExtendedProjectionLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingExtendedProjectionLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingDuplicateRecursiveLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingDuplicateRecursiveLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingExtendedRecursiveLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingExtendedRecursiveLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingCrossGroupMismatchLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingCrossGroupMismatchLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingTransitiveMismatchLinkSource([]byte) uint8 { return 0 }
func railshotConfigureGCTypeSubtypingTransitiveMismatchLink(uint8, *railshotCompileOptions) {
}
func railshotGCTypeSubtypingNonFlatExportSource([]byte) bool { return false }
func railshotConfigureGCTypeSubtypingNonFlatExport(bool, *railshotCompileOptions) {
}
func railshotGCRootedScalarSource([]byte) bool                              { return false }
func railshotGCStructGetSource([]byte) bool                                 { return false }
func railshotConfigureGCStructGet(bool, *railshotCompileOptions)            {}
func railshotGCStructMutationSource([]byte) bool                            { return false }
func railshotConfigureGCStructMutation(bool, *railshotCompileOptions)       {}
func railshotGCStructNullSource([]byte) bool                                { return false }
func railshotConfigureGCStructNull(bool, *railshotCompileOptions)           {}
func railshotGCStructPackedSource([]byte) bool                              { return false }
func railshotConfigureGCStructPacked(bool, *railshotCompileOptions)         {}
func railshotGCStructBasicSource([]byte) bool                               { return false }
func railshotConfigureGCStructBasic(bool, *railshotCompileOptions)          {}
func railshotGCStructNumericGlobalsSource([]byte) bool                      { return false }
func railshotConfigureGCStructNumericGlobals(bool, *railshotCompileOptions) {}
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
	return nil, fmt.Errorf("source compilation is unavailable in a standalone executable")
}

func railshotHostIndirectThunk(index uint32) []byte { return railshot.HostIndirectThunk(index) }

func railshotHostIndirectSyncThunk(index uint32, params, results int) []byte {
	return railshot.HostIndirectSyncThunk(index, params, results)
}

func railshotHostIndirectOwnedSyncThunk(index uint32, params, results int) []byte {
	return railshot.HostIndirectOwnedSyncThunk(index, params, results)
}

func railshotFunctionSubtypeHelpers(*wasm.Module) bool { return false }
