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
	return boundedModuleHostCode(m, true)
}

func boundedModuleHostCode(m *wasm.Module, native bool) bool {
	imports := m.ImportedFuncCount()
	if imports == 0 || imports > 64 || native && imports != 1 || len(m.Code) == 0 {
		return false
	}
	for i, fn := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok || len(ft.Params) > 64 || len(ft.Results) > 64 {
			return false
		}
		for _, typ := range ft.Params {
			if typ.Kind() != wasm.ValNum {
				return false
			}
		}
		for _, typ := range ft.Results {
			if typ.Kind() != wasm.ValNum {
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
		proof := shared.BoundedHostSegments
		if native {
			proof = shared.BoundedNativeHostBody
		}
		if locals > 64 || !proof(fn.BodyBytes, uint32(imports)) {
			return false
		}
	}
	return true
}

func (in *Instance) hasBoundedCallerHostView() bool {
	if !in.singleTypedScalarHostEligible() {
		return false
	}
	binding := &in.syncHosts[0]
	// Owned HostFuncRef imports use a separate dispatch namespace. Their
	// ordinary path remains responsible for owner identity and signature.
	_, caller := binding.fn.(CallerHostCallFunc)
	return caller && binding.gate == nil && binding.scalarKind != syncHostNonScalar &&
		len(binding.sig.Params) <= coreruntime.MaxHostArity && len(binding.sig.Results) <= coreruntime.MaxHostArity
}

// A lowered native host leaf cannot serve as a Go safe point. Even if the
// compiler found host-yielding segments, this instance needs syscall scheduling.
func (in *Instance) boundedHostSegments() bool {
	return in.c.boundedHostSegments() && in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0
}

// A bounded native-only module can use isolated entry without parked Go state.
// Resource publication revokes the existing fast gate before sharing context.
// Unknown bindings, mutable reference topology and external backing fail closed.
func (in *Instance) isolatedNativeScalarLeaf() bool {
	if in == nil || in.c == nil || !in.c.boundedNativeScalarLeaf() || in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 || !in.preparedFastStateValid() || !in.usesIndependentExecution() || codeProfileEnabled {
		return false
	}
	if in.memoryDir != nil || len(in.globalCells) != 0 || in.tableDescPtr != 0 || in.gc != nil || in.c.needsFuncRefContext() || len(in.hostLog) != 0 {
		return false
	}
	if in.refStore != nil && !in.refStore.private {
		return false
	}
	if in.memory != nil {
		if !in.ownsMem {
			return false
		}
		_, shared := in.memory.importShape()
		if shared {
			return false
		}
	}
	return true
}

// Integer-only guest execution cannot observe or modify FP control/status.
// ARM64 integer popcount lowers through the SIMD allocator. A register-free
// guest certificate must exclude it even though it does not affect FP control.
func boundedIntegerModuleHostSegmentsFor(m *wasm.Module, noSIMD bool) bool {
	if !boundedModuleHostSegments(m) {
		return false
	}
	integer := func(t wasm.ValType) bool { return t == wasm.I32 || t == wasm.I64 }
	for i := 0; i < m.FuncCount(); i++ {
		ft, ok := m.FuncSignature(uint32(i))
		if !ok {
			return false
		}
		for _, t := range ft.Params {
			if !integer(t) {
				return false
			}
		}
		for _, t := range ft.Results {
			if !integer(t) {
				return false
			}
		}
	}
	for _, fn := range m.Code {
		for _, local := range fn.Locals.Runs {
			if !integer(local.Type) {
				return false
			}
		}
		r := wasm.ReaderFrom(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var imm wasm.InstructionImmediate
			if wasm.ClassifyInstructionImmediateInto(&r, op, &imm) != nil {
				return false
			}
			if noSIMD && (op == 0x69 || op == 0x7b) {
				return false
			}
			switch {
			case op < 0x41: // control, locals, imported calls; established by the CFG proof
			case op == 0x41 || op == 0x42:
			case op >= 0x45 && op <= 0x5a: // integer comparisons
			case op >= 0x67 && op <= 0x8a: // integer ALU
			case op == 0xa7 || op == 0xac || op == 0xad: // integer width conversion
			case op >= 0xc0 && op <= 0xc4: // integer sign extension
			default:
				return false
			}
		}
	}
	return true
}
