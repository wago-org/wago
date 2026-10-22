//go:build linux && !tinygo

package riscv64

import (
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
	"slices"
)

// Source plans include dead sites. Once every source site has been accounted
// for, retain only masks for emitted sites. The independently validated source
// plan is conservative: every mask contains every tracked local, so removing
// a dead site cannot change the local roots of a later live site.
func finalizeGCFrameRootMasks(plan *shared.GCFrameRootPlan, deadAllocations, deadCalls uint32) bool {
	if plan == nil || deadAllocations == 0 && deadCalls == 0 {
		return true
	}
	if !plan.Exact || !plan.Conservative || plan.SafepointCount()+int(deadAllocations) != plan.AllocationMaskCount() || plan.CallsiteCount()+int(deadCalls) != plan.CallMaskCount() {
		return false
	}
	wordsPerSite := (len(plan.Locals) + 63) / 64
	if wordsPerSite == 0 {
		wordsPerSite = 1
	}
	words := make([]uint64, wordsPerSite*(plan.SafepointCount()+plan.CallsiteCount()))
	for site := 0; site < plan.SafepointCount()+plan.CallsiteCount(); site++ {
		for local := range plan.Locals {
			words[site*wordsPerSite+local/64] |= uint64(1) << (local % 64)
		}
	}
	return plan.SetLiveMasks(words, plan.SafepointCount(), plan.CallsiteCount())
}

// validateGCFrameRootPlans independently authenticates the initial RV64
// frontend/backend handshake. This stage deliberately requires every collector
// local at every allocation site; later liveness-exact admission can replace
// this conservative invariant once its analysis is shared with the backend.
func validateGCFrameRootPlans(m *wasm.Module, plans *shared.GCModuleFrameRootPlan) bool {
	return validateGCFrameRootPlansOwned(m, plans, false)
}

func validateGCFrameRootPlansOwned(m *wasm.Module, plans *shared.GCModuleFrameRootPlan, ownedExceptions bool, custom ...map[uint32]railcore.CustomInstruction) bool {
	var instructions map[uint32]railcore.CustomInstruction
	if len(custom) != 0 {
		instructions = custom[0]
	}
	if err := admitCustomInstructions(m, instructions); err != nil {
		return false
	}
	customValues := pluginInstructions(instructions).hasCustom()
	if m == nil || plans == nil || plans.FunctionCount() != len(m.Code) {
		return false
	}
	functionEnvironments := ownedExceptions || wasm.ModuleFunctionReferenceCarriers(m)
	ehMaps, err := BuildExceptionRootMapsOwned(m, ownedExceptions)
	if err != nil {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for function := range m.Code {
		usesSWAR, err := functionUsesSWAR(m, function)
		usesSWAR = usesSWAR || customValues
		if err != nil {
			return false
		}
		if plans.FunctionPending(function) {
			return false
		}
		allocations, calls := 0, 0
		if ownedExceptions {
			count, err := ExceptionOwnedCopyCount(m, function)
			if err != nil {
				return false
			}
			allocations = count
		}
		tailBridge := false
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
				if _, inline := instructions[imm.Index]; !inline {
					calls++
				}
			case wasm.InstrCallIndirect:
				calls++
			case wasm.InstrCallRef:
				if !rootedScalarReferenceCall(m, imm) {
					return false
				}
				calls++
			case wasm.InstrReturnCallRef:
				if !rootedScalarNumericReferenceTail(m, imm) {
					return false
				}
				tailBridge = true
			case wasm.InstrReturnCallIndirect:
				if !rootedScalarNumericIndirectTail(m, imm) {
					return false
				}
				tailBridge = true
			case wasm.InstrReturnCall:
				if uint64(imm.Index) >= uint64(m.FuncCount()) {
					return false
				}
				if uint64(imm.Index) < uint64(m.ImportedFuncCount()) {
					sig, ok := m.FuncSignature(imm.Index)
					if !ok || !rootedScalarTailSignature(m, sig) {
						return false
					}
					tailBridge = true
				}
			}
			if op == 0xfb {
				switch imm.Subopcode {
				case 0, 1, 6, 7, 8, 9, 10, 26, 27:
					allocations++
				}
			}
			if wasm.OpaqueStorageMutation(m, imm.Kind, imm.Index, imm.Index2) {
				allocations++
			}
		}
		plan := plans.Function(function)
		var fixed []uint32
		for _, rootMap := range ehMaps {
			if rootMap.LocalFunction == uint32(function) {
				for _, slot := range rootMap.Slots {
					if slot.Kind == nativeabi.RootGCRef {
						fixed = append(fixed, slot.Offset)
					}
				}
			}
		}
		if plan != nil && !slices.Equal(plan.FixedOffsets(), fixed) {
			return false
		}
		if allocations == 0 && calls == 0 {
			if !tailBridge {
				if plan != nil {
					return false
				}
				continue
			}
			if plan == nil || !plan.Candidate || !plan.Exact || !plan.Conservative || plan.AllocationMaskCount() != 0 || plan.CallMaskCount() != 0 || len(plan.Locals) != 0 || !plan.ValidLiveMasks() {
				return false
			}
			continue
		}
		if plan == nil || !plan.Candidate || !plan.Exact || !plan.Conservative || plan.AllocationMaskCount() != allocations || plan.CallMaskCount() != calls || !plan.ValidLiveMasks() {
			return false
		}
		locals, ok := rv64GCFrameLocalsLayoutOwned(m, function, usesSWAR, functionEnvironments)
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
	return rv64GCFrameLocalsLayoutOwned(m, function, usesSWAR, false)
}

func rv64GCFrameLocalsLayoutOwned(m *wasm.Module, function int, usesSWAR, ownedExceptions bool) ([]shared.GCFrameLocal, bool) {
	ft, ok := m.LocalFuncType(function)
	if !ok {
		return nil, false
	}
	locals := make([]shared.GCFrameLocal, 0)
	slot, index := 0, uint32(0)
	add := func(t wasm.ValType) bool {
		functionRef := ownedExceptions && wasm.FunctionReferenceType(m, t)
		externRef := wasm.ExternReferenceType(t)
		if rv64CollectorFrameRefType(m, t) || ownedExceptions && exceptionReferenceType(t) || functionRef || externRef {
			if len(locals) == shared.GCFrameTrackedLocalLimit {
				return false
			}
			offset := uint32(shared.RV64CallFrameHeaderBytes + slot*8)
			if functionRef {
				offset |= shared.GCFrameFuncRefRootFlag
			} else if externRef {
				offset |= shared.GCFrameExternRefRootFlag
			}
			locals = append(locals, shared.GCFrameLocal{Index: index, Offset: offset})
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
