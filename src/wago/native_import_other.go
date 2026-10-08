//go:build !riscv64

package wago

import (
	"context"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func railshotInvokeNativeImportedStart(*Instance, int, map[uint32]uint64, context.Context) (bool, error) {
	return false, nil
}

// These targets preserve their established Runtime terminal-observer ordering.
func railshotCheckImportedStartCompletion(*Instance, context.Context) error { return nil }

func railshotNativeContextTrapABI() bool                               { return false }
func railshotNativeImportArenaBytes(*Compiled, resolvedImports) int    { return 0 }
func railshotNativeContextIdentity(*coreruntime.JobMemory) uintptr     { return 0 }
func railshotSetNativeContextIdentity(*coreruntime.JobMemory, uintptr) {}
func railshotAlignNativeImportBlob(blob []byte) []byte                 { return blob }
func railshotNativeImportCode(*Compiled, *InstanceExport, *coreruntime.Arena, uintptr, uintptr, int, *railshotNativeImportConstruction) ([]byte, bool, error) {
	return nil, false, nil
}
func railshotTrapOwner(root *Instance, _ *coreruntime.TrapError) (*Instance, bool) { return root, true }

func railshotImportedCallSlots(_ *Compiled, params, results int) (int, int, error) {
	return params, results, nil
}

func railshotInvokeNativeReexport(*Instance, string, int, []uint64, invocationContextSet) ([]uint64, bool, error) {
	return nil, false, nil
}

func railshotResolveNativeHostControl(*Instance, uintptr) *Instance { return nil }

func railshotRegisterNativeHostOwner(*Instance) error { return nil }
func railshotUnregisterNativeHostOwner(*Instance)     {}

type railshotInvocationState struct{}

func railshotSetInvocationGateOwner(*instancePluginState, invocationID) {}
func railshotCanReenter(*Instance, invocationID) bool                   { return true }

func railshotCallbackHostScope(_ *Instance, _ uintptr, ordinary *hostCallScope) *hostCallScope {
	return ordinary
}

func railshotCanonicalImportedFuncref(*referenceStore, *Instance, *InstanceExport, int, uint64) bool {
	return false
}
func railshotFuncRefMatchesFunction(*Instance, FuncRef, uint32) bool { return false }

func railshotCanonicalHostFuncref(*referenceStore, *Instance, *HostFuncRef, int, uint64) (*Instance, uint64, bool) {
	return nil, 0, false
}

type importGlobalReference struct{}

func railshotFreezeGlobalReferences(*Compiled, []*resolvedGlobalImport, *referenceStore) ([]importGlobalReference, error) {
	return nil, nil
}
func railshotReleaseGlobalReferences([]importGlobalReference) {}
func railshotImportedGlobalValue(*Instance, *Global) (Value, bool, error) {
	return Value{}, false, nil
}
func railshotCanonicalGlobalFuncref(*referenceStore, *Instance, uint64) (*Instance, uint64, bool) {
	return nil, 0, false
}

type railshotNativeImportConstruction struct{}

func railshotNativeImportMetadataBytes(*Compiled, resolvedImports) int { return 0 }
func railshotInitNativeImportConstruction(*railshotNativeImportConstruction, *Compiled, resolvedImports) {
}
func railshotRecordNativeImportSpan(*railshotNativeImportConstruction, int, int, int) {}
func railshotFinishNativeImportConstruction(*railshotNativeImportConstruction, []byte, uintptr, int) {
}
func railshotInstallNativeImportConstruction(*Instance, *railshotNativeImportConstruction) {}
