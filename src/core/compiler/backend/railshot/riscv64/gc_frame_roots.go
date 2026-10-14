//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// validateGCFrameRootPlans independently authenticates the initial RV64
// frontend/backend handshake. This stage deliberately requires every collector
// local at every allocation site; later liveness-exact admission can replace
// this conservative invariant once its analysis is shared with the backend.
func validateGCFrameRootPlans(m *wasm.Module, plans *shared.GCModuleFrameRootPlan) bool {
	if m == nil || plans == nil || plans.FunctionCount() != len(m.Code) {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for function := range m.Code {
		usesSWAR, err := functionUsesSWAR(m, function)
		if err != nil {
			return false
		}
		if plans.FunctionPending(function) {
			return false
		}
		allocations, calls := 0, 0
		r := wasm.NewReader(m.Code[function].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				return false
			}
			switch imm.Kind {
			case wasm.InstrCall:
				calls++
			case wasm.InstrCallIndirect:
				calls++
			case wasm.InstrCallRef,
				wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
				return false
			}
			if op == 0xfb {
				switch imm.Subopcode {
				case 0, 1, 6, 7, 8, 9, 10:
					allocations++
				}
			}
		}
		plan := plans.Function(function)
		if allocations == 0 && calls == 0 {
			if plan != nil {
				return false
			}
			continue
		}
		if plan == nil || !plan.Candidate || !plan.Exact || !plan.Conservative || plan.AllocationMaskCount() != allocations || plan.CallMaskCount() != calls || plan.HasFixedOffsets() || !plan.ValidLiveMasks() {
			return false
		}
		locals, ok := rv64GCFrameLocalsLayout(m, function, usesSWAR)
		if !ok || len(locals) != len(plan.Locals) {
			return false
		}
		for i := range locals {
			if locals[i] != plan.Locals[i] {
				return false
			}
		}
		for site := 0; site < allocations; site++ {
			live := 0
			if !plan.VisitLiveLocals(site, false, func(int) { live++ }) || live != len(plan.Locals) {
				return false
			}
		}
		for site := 0; site < calls; site++ {
			live := 0
			if !plan.VisitLiveLocals(site, true, func(int) { live++ }) || live != len(plan.Locals) {
				return false
			}
		}
	}
	return true
}

func rv64GCFrameLocals(m *wasm.Module, function int) ([]shared.GCFrameLocal, bool) {
	usesSWAR, err := functionUsesSWAR(m, function)
	if err != nil {
		return nil, false
	}
	return rv64GCFrameLocalsLayout(m, function, usesSWAR)
}

func rv64GCFrameLocalsLayout(m *wasm.Module, function int, usesSWAR bool) ([]shared.GCFrameLocal, bool) {
	ft, ok := m.LocalFuncType(function)
	if !ok {
		return nil, false
	}
	locals := make([]shared.GCFrameLocal, 0)
	slot, index := 0, uint32(0)
	add := func(t wasm.ValType) bool {
		if rv64CollectorFrameRefType(m, t) {
			if len(locals) == shared.GCFrameTrackedLocalLimit {
				return false
			}
			locals = append(locals, shared.GCFrameLocal{Index: index, Offset: uint32(shared.RV64CallFrameHeaderBytes + slot*8)})
		}
		// SWAR uses two physical qwords for every logical local so scalar and
		// reference homes cannot overlap either half of a vector carrier.
		if usesSWAR || wasm.EqualValType(t, wasm.V128) {
			slot += 2
		} else {
			slot++
		}
		index++
		return true
	}
	for _, typ := range ft.Params {
		if !add(typ) {
			return nil, false
		}
	}
	for _, run := range m.Code[function].Locals.Runs {
		for i := uint32(0); i < run.Count; i++ {
			if !add(run.Type) {
				return nil, false
			}
		}
	}
	return locals, true
}

func rv64CollectorFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind() != wasm.ValRef {
		return false
	}
	heap := t.Ref().Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
			return true
		}
	case wasm.HeapDefType:
		kind, valid := heap.DefCompKind()
		return !valid || kind == wasm.CompStruct || kind == wasm.CompArray
	case wasm.HeapTypeIndex:
		index := heap.Type().Index
		for _, group := range m.Types {
			if index < uint32(len(group.SubTypes)) {
				kind := group.SubTypes[index].Comp.Kind
				return kind == wasm.CompStruct || kind == wasm.CompArray
			}
			index -= uint32(len(group.SubTypes))
		}
		return true
	default:
		return true
	}
	return false
}
