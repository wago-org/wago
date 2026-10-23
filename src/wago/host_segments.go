package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

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
	if !coreruntime.NativeScalarLeafSupported || in == nil || in.c == nil || !in.c.boundedNativeScalarLeaf() || in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 || !in.preparedFastStateValid() || !in.usesIndependentExecution() || codeProfileEnabled {
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

// The selected CFG proof separately excludes helpers and unsupported memory operations.
func integerHostModuleRegistersFor(m *wasm.Module, noSIMD bool) bool {
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
			case op == 0x2a || op == 0x2b || op == 0x38 || op == 0x39: // FP memory accesses
				return false
			case op < 0x41: // control, locals, integer memory, imported calls; CFG proved separately
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

func (in *Instance) boundedFixedSharedHostPortal() bool {
	// Keep resource-free bridge selection unchanged. This certificate only
	// removes syscall scheduling around the existing full portal dispatcher.
	return in.c.fixedSharedHostSegments && in.threadedMemoryZero &&
		in.memoryDir != nil && len(in.memoryDir.memories) == 1 && len(in.memoryDir.owns) == 1 && in.memoryDir.owns[0] &&
		in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0
}
