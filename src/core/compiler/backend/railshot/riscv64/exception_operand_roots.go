//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func exceptionOpaqueReferences(m *wasm.Module) bool {
	opaque := func(typ wasm.ValType) bool { return typ.Kind() == wasm.ValRef && !wasm.CollectorReferenceType(m, typ) }
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			for _, values := range [][]wasm.ValType{sub.Comp.Params, sub.Comp.Results} {
				for _, typ := range values {
					if opaque(typ) {
						return true
					}
				}
			}
		}
	}
	for i := 0; i < m.ImportedGlobalCount()+len(m.Globals); i++ {
		if global, ok := m.GlobalTypeByIndex(uint32(i)); ok && opaque(global.Type) {
			return true
		}
	}
	for i := 0; i < m.TableCount(); i++ {
		if table, ok := m.TableType(uint32(i)); ok && opaque(wasm.RefVal(table.Ref)) {
			return true
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for _, fn := range m.Code {
		for _, run := range fn.Locals.Runs {
			if opaque(run.Type) {
				return true
			}
		}
		r := wasm.ReaderFrom(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return true
			}
			var imm wasm.InstructionImmediate
			if classifier.ClassifyInto(&r, op, &imm) != nil {
				return true
			}
			if imm.Kind == wasm.InstrAnyConvertExtern || imm.Kind == wasm.InstrExternConvertAny || imm.Kind == wasm.InstrRefFunc || imm.Kind == wasm.InstrRefNull && imm.HasHeapType && opaque(wasm.RefVal(wasm.Ref(true, imm.HeapType, false))) {
				return true
			}
		}
	}
	return false
}

func (mode emissionMode) functionEnvironmentRoots() bool {
	return mode.ownedExceptionReferences || mode.gcFunctionEnvironments
}

func (mode emissionMode) opaqueExternRoots() bool {
	return mode.gcRootedScalar || mode.ownedExceptionReferences
}

func (f *integerEmitter) functionMutationSafepoint(roots [][]bool) uint32 {
	if len(roots) == 0 {
		return 0
	}
	return f.recordGCFrameSafepoint(roots[0])
}

func (f *integerEmitter) functionMutation(kind wasm.InstrKind, typeIndex, fieldIndex uint32) bool {
	if !f.mode.functionEnvironmentRoots() && !f.mode.opaqueExternRoots() {
		return false
	}
	m := f.mode.tableModuleSource
	if m == nil {
		m = f.mode.exceptionModule
	}
	return f.mode.functionEnvironmentRoots() && wasm.FunctionStorageMutation(m, kind, typeIndex, fieldIndex) || f.mode.opaqueExternRoots() && wasm.ExternStorageMutation(m, kind, typeIndex, fieldIndex)
}

func (f *integerEmitter) externPersistentRoots(kind wasm.InstrKind, index, index2 uint32, refs []bool) [][]bool {
	m := f.mode.tableModuleSource
	if m == nil {
		m = f.mode.exceptionModule
	}
	if f.mode.opaqueExternRoots() && wasm.ExternPersistentMutation(m, kind, index, index2) {
		return [][]bool{refs}
	}
	return nil
}

func prepareCollectorOperandPlans(m *wasm.Module, opts *CompileOptions) error {
	if opts.GCFrameRoots == nil || !opts.gcFunctionEnvironments && (!(opts.OwnedExceptionReferences || opts.gcRootedScalar) || !exceptionOpaqueReferences(m)) {
		return nil
	}
	var selected []bool
	for function := range m.Code {
		if opts.GCFrameRoots.Function(function) != nil {
			if selected == nil {
				selected = make([]bool, len(m.Code))
			}
			selected[function] = true
		}
	}
	if selected == nil {
		return nil
	}
	plans, err := wasm.BuildCollectorOperandRootModule(m, selected)
	if err != nil {
		return err
	}
	opts.exceptionOperands = &plans
	return nil
}

func (f *integerEmitter) prepareCollectorOperands(m *wasm.Module, fn *wasm.Func, swar bool) error {
	if m == nil || !(f.mode.functionEnvironmentRoots() || f.mode.opaqueExternRoots()) || f.gcFrameRoots == nil || f.mode.exceptionOperands == nil && !f.mode.gcFunctionEnvironments && !exceptionOpaqueReferences(m) {
		return nil
	}
	function := -1
	for i := range m.Code {
		if &m.Code[i] == fn {
			function = i
			break
		}
	}
	if function < 0 {
		return unsupported("exception collector function identity")
	}
	var plan *wasm.CollectorOperandRoots
	if f.mode.exceptionOperands != nil {
		plans := *f.mode.exceptionOperands
		if function >= len(plans) || plans[function] == nil {
			return unsupported("missing exception collector function plan")
		}
		plan = plans[function]
	} else {
		var err error
		plan, err = wasm.BuildCollectorOperandRoots(m, function)
		if err != nil {
			return err
		}
	}
	f.gcOperandRoots, f.gcOperandStride = plan, 1
	if swar {
		f.gcOperandStride = 2
	}
	return nil
}

func (f *integerEmitter) collectorOperandSlots(hidden []bool, pc uint32, visit func(int)) bool {
	if len(hidden) == 0 {
		return true
	}
	if f.gcOperandRoots == nil {
		for slot, root := range hidden {
			if root {
				visit(slot)
			}
		}
		return true
	}
	roots, ok := f.gcOperandRoots.At(pc)
	if !ok {
		f.err = unsupported("missing typed exception operand roots")
		return false
	}
	for _, logical := range roots {
		slot := int(logical) * f.gcOperandStride
		if f.customSpillOffsets != nil {
			if int(logical) >= len(f.customSpillOffsets) {
				f.err = unsupported("exception collector operand home")
				return false
			}
			slot = int(f.customSpillOffsets[logical] / 8)
		}
		// Native custom carriers keep their own class and never become roots
		// merely because their erased Wasm type is a reference carrier.
		if slot < len(hidden) && hidden[slot] {
			visit(slot)
		}
	}
	return true
}

func (f *integerEmitter) referenceOperandOffsets(hidden []bool, pc uint32, visit func(uint32)) bool {
	if !f.collectorOperandSlots(hidden, pc, func(slot int) { visit(uint32(f.SpillOffset(slot))) }) {
		return false
	}
	if len(hidden) == 0 || f.gcOperandRoots == nil {
		return true
	}
	if f.mode.functionEnvironmentRoots() && !f.opaqueOperandOffsets(hidden, pc, shared.GCFrameFuncRefRootFlag, visit) {
		return false
	}
	return !f.mode.opaqueExternRoots() || f.opaqueOperandOffsets(hidden, pc, shared.GCFrameExternRefRootFlag, visit)
}

func (f *integerEmitter) opaqueOperandOffsets(hidden []bool, pc, flag uint32, visit func(uint32)) bool {
	var roots []uint32
	var ok bool
	if flag == shared.GCFrameFuncRefRootFlag {
		roots, ok = f.gcOperandRoots.FunctionsAt(pc)
	} else {
		roots, ok = f.gcOperandRoots.ExternsAt(pc)
	}
	if !ok {
		f.err = unsupported("missing typed function operand roots")
		return false
	}
	for _, logical := range roots {
		slot := int(logical) * f.gcOperandStride
		if f.customSpillOffsets != nil {
			if int(logical) >= len(f.customSpillOffsets) {
				f.err = unsupported("exception function operand home")
				return false
			}
			slot = int(f.customSpillOffsets[logical] / 8)
		}
		if slot < len(hidden) && hidden[slot] {
			visit(uint32(f.SpillOffset(slot)) | flag)
		}
	}
	return true
}
