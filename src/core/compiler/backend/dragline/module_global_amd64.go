//go:build amd64

package dragline

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// selectAMD64ModuleGlobalPin makes one module-wide choice from the validated
// structured instructions. A loop-weighted aggregate prevents one leaf's
// local reuse from reserving RBP in every other function of the module.
// A module with any structured fallback is left on the ordinary ABI.
func selectAMD64ModuleGlobalPin(m *wasm.Module, moduleHasV128, cachelessSignals bool) (amd64ModuleGlobalPin, bool) {
	if m == nil || m.GlobalCount() == 0 || len(m.Code) == 0 || m.TagCount() != 0 {
		return amd64ModuleGlobalPin{}, false
	}
	scores := make([]uint64, m.GlobalCount())
	accesses := make([]uint64, m.GlobalCount())
	var foreignCallSites uint64
	var scratch railssa.StackFunc
	for i := range m.Code {
		stack, err := railssa.BuildStackFuncInto(m, i, &scratch)
		if err != nil || stack.HasReferences || !amd64RailMachSerialCandidate(stack, moduleHasV128, len(m.Globals) >= amd64RailMachDenseGlobalThreshold, cachelessSignals) {
			return amd64ModuleGlobalPin{}, false
		}
		depthDelta := make([]int16, len(stack.Instrs)+1)
		for _, region := range stack.Regions {
			if region.Kind == wasm.InstrLoop && region.EndInstr > region.StartInstr && int(region.EndInstr) < len(stack.Instrs) {
				depthDelta[region.StartInstr+1]++
				depthDelta[region.EndInstr]--
			}
		}
		depth := int16(0)
		for instructionID, instruction := range stack.Instrs {
			depth += depthDelta[instructionID]
			if instruction.Kind == wasm.InstrCallIndirect || instruction.Kind == wasm.InstrCallRef ||
				instruction.Kind == wasm.InstrReturnCallIndirect || instruction.Kind == wasm.InstrReturnCallRef ||
				instruction.Kind == wasm.InstrCall && instruction.U32() < stack.ImportedFuncs ||
				instruction.Kind == wasm.InstrReturnCall && instruction.U32() < stack.ImportedFuncs {
				foreignCallSites++
			}
			if instruction.Kind != wasm.InstrGlobalGet && instruction.Kind != wasm.InstrGlobalSet {
				continue
			}
			index := instruction.U32()
			if int(index) >= len(scores) {
				continue
			}
			accesses[index]++
			weight := uint64(1)
			for level := int16(0); level < depth && level < 6; level++ {
				weight *= 10
			}
			if scores[index] > math.MaxUint64-weight {
				scores[index] = math.MaxUint64
			} else {
				scores[index] += weight
			}
		}
	}
	var best amd64ModuleGlobalPin
	bestScore := uint64(0)
	for index, score := range scores {
		global, ok := m.GlobalTypeByIndex(uint32(index))
		if !ok || index < m.ImportedGlobalCount() || !global.Mutable || global.Type != wasm.I32 && global.Type != wasm.I64 {
			continue
		}
		exported := false
		for _, export := range m.Exports {
			if export.Index.Kind == wasm.ExternGlobal && export.Index.Index == uint32(index) {
				exported = true
				break
			}
		}
		if exported {
			continue
		}
		if score > bestScore {
			best, bestScore = amd64ModuleGlobalPin{global: uint32(index), typ: global.Type}, score
		}
	}
	// Each foreign/indirect call can require three dependent loads to restore
	// the value, whereas an in-module access saves at most two. A reservation
	// also removes RBP from every carrying function, so require substantial
	// module-wide static reuse before paying that register-pressure cost.
	return best, bestScore >= 30 && accesses[best.global] >= 128 && accesses[best.global] >= 3*(foreignCallSites+1)
}

// A call-free function that never touches the pin can use the ordinary RBP
// allocation and callee-save contract. Its caller's module value is restored
// before control returns; no private callee can observe its temporary RBP.
func amd64FunctionCarriesModuleGlobalPin(stack *railssa.StackFunc, pin *amd64ModuleGlobalPin) bool {
	if stack == nil || pin == nil {
		return false
	}
	for _, instruction := range stack.Instrs {
		switch instruction.Kind {
		case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
			if instruction.U32() == pin.global {
				return true
			}
		case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrCallRef,
			wasm.InstrReturnCall, wasm.InstrReturnCallIndirect, wasm.InstrReturnCallRef:
			return true
		}
	}
	return false
}
