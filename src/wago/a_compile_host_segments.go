package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// All native functions in an admitted module obey the segment proof, so any
// locally resumed continuation has the same bound. Runtime admission separately
// requires every selected import to be an admitted numeric Go callback.
// Native-only bodies retain the stricter single-import contract.
func boundedModuleHostSegments(m *wasm.Module) bool {
	return boundedModuleHostCode(m, false)
}

func boundedModuleNativeHostBody(m *wasm.Module) bool {
	return coreruntime.NativeScalarLeafSupported && boundedModuleHostCode(m, true)
}

func boundedModuleHostCode(m *wasm.Module, native bool) bool {
	imports := m.ImportedFuncCount()
	if imports == 0 || imports > 64 || native && imports != 1 || len(m.Code) == 0 {
		return false
	}
	for i, fn := range m.Code {
		if !numericHostFunction(m, i, fn) {
			return false
		}
		proof := shared.BoundedHostSegments
		if coreruntime.NativeScalarLeafSupported && native {
			proof = shared.BoundedNativeHostBody
		}
		if !proof(fn.BodyBytes, uint32(imports)) {
			return false
		}
	}
	return true
}

// Integer-only guest execution cannot observe or modify FP control/status.
// ARM64 integer popcount lowers through the SIMD allocator. A register-free
// guest certificate must exclude it even though it does not affect FP control.
func boundedIntegerModuleHostSegmentsFor(m *wasm.Module, noSIMD bool) bool {
	return boundedModuleHostSegments(m) && integerHostModuleRegistersFor(m, noSIMD)
}

func numericHostFunction(m *wasm.Module, index int, fn wasm.Func) bool {
	ft, ok := m.LocalFuncType(index)
	if !ok || len(ft.Params) > 64 || len(ft.Results) > 64 {
		return false
	}
	for _, t := range ft.Params {
		if t.Kind() != wasm.ValNum {
			return false
		}
	}
	for _, t := range ft.Results {
		if t.Kind() != wasm.ValNum {
			return false
		}
	}
	var locals uint64
	for _, run := range fn.Locals.Runs {
		if run.Type.Kind() != wasm.ValNum {
			return false
		}
		locals += uint64(run.Count)
	}
	return locals <= 64
}

// This is a scheduler certificate, not the resource-free private Go bridge
// certificate. Only known-yielding local callees cut edges in their callers.
func boundedFixedSharedHostSegments(m *wasm.Module) bool {
	if m == nil || m.ImportedFuncCount() != 1 || len(m.Imports) != 1 || len(m.Memories) != 1 || len(m.Tables) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.Code) == 0 || len(m.Code) > 8 {
		return false
	}
	memory := m.Memories[0]
	if !memory.Shared || memory.Limits.Addr64 || !memory.Limits.HasMax || memory.Limits.Min != memory.Limits.Max {
		return false
	}
	for i, fn := range m.Code {
		if !numericHostFunction(m, i, fn) {
			return false
		}
	}
	var bounded [8]bool
	var yielding uint64
	for pass := 0; pass < len(m.Code); pass++ {
		progress := false
		for i, fn := range m.Code {
			if bounded[i] {
				continue
			}
			ok, yields := shared.BoundedFixedSharedHostSegments(fn.BodyBytes, 1, yielding)
			if !ok {
				continue
			}
			bounded[i] = true
			progress = true
			if yields {
				yielding |= uint64(1) << uint(i)
			}
		}
		if !progress {
			break
		}
	}
	for i := range m.Code {
		if !bounded[i] {
			return false
		}
	}
	return true
}
