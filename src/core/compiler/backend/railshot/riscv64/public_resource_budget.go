//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Native source PCs and frame packets use signed 32-bit byte offsets.
const MaxPublicCodeBytes = 1<<31 - 1

// Public metadata is bounded by its serialized extent and the configured quota.
// Private compilation retains its small independently owned state profile.
const MaxPublicGlobals = MaxPublicCodeBytes / 16
const MaxPublicImports = MaxPublicCodeBytes / 16
const MaxPublicElementSegments = MaxPublicCodeBytes / 16

func productionElementSegmentLimit(public bool) int {
	if public {
		return MaxPublicElementSegments
	}
	return MaxOwnedElementSegments
}

func productionImportLimit(public bool) int {
	if public {
		return MaxPublicImports
	}
	return MaxProductionImports
}

func productionGlobalLimit(public bool) int {
	if public {
		return MaxPublicGlobals
	}
	return 256
}

func publicModuleByteLimit(opts CompileOptions) int {
	if opts.MaxModuleBytes == 0 || opts.MaxModuleBytes > MaxPublicCodeBytes {
		return MaxPublicCodeBytes
	}
	return int(opts.MaxModuleBytes)
}

func publicCodeByteLimit(opts CompileOptions) int {
	// Final layout can be smaller than the staging image. The public adapter
	// applies the configured quota once after all module code is finalized.
	return MaxPublicCodeBytes
}

// Bound new function/type/state directories before metadata expansion. Source
// decoding and frozen metadata apply their own exact configured byte quotas.
func admitPublicDecodedDirectoryBudget(m *wasm.Module, opts CompileOptions) error {
	if m != nil && opts.MaxNativeCodeBytes != 0 && uint64(len(m.Code)) > opts.MaxNativeCodeBytes/4 {
		return &coreruntime.ResourceLimitError{Resource: "native code bytes", Scope: "compile", Requested: uint64(len(m.Code)) * 4, Limit: opts.MaxNativeCodeBytes}
	}
	remaining := opts.MaxCompiledMetadataBytes
	if remaining == 0 {
		remaining = 256 << 20
	}
	limit := remaining
	consume := func(count int, width uint64) bool {
		if count < 0 || width != 0 && uint64(count) > remaining/uint64(width) {
			return false
		}
		remaining -= uint64(count) * uint64(width)
		return true
	}
	// Each published function retains entry offsets; compact data needs at
	// least one 16-byte record. These are lower bounds, not temporary sizes.
	if m == nil || !consume(len(m.Code)+m.ImportedFuncCount(), 16) || !consume(len(m.Globals)+m.ImportedGlobalCount(), 16) || !consume(len(m.Data), 16) {
		return &coreruntime.ResourceLimitError{Resource: "metadata bytes", Scope: "compiled snapshot", Requested: limit + 1, Limit: limit}
	}
	for _, group := range m.Types {
		if !consume(len(group.SubTypes), 4) {
			return &coreruntime.ResourceLimitError{Resource: "metadata bytes", Scope: "compiled snapshot", Requested: limit + 1, Limit: limit}
		}
	}
	return nil
}
