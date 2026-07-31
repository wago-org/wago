//go:build linux && amd64

package wago

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// newGCFrameRootPlan admits import/global/start/table/element/tag/EH-free local
// call graphs with numeric native-call signatures and at most 64 collector roots
// per function. Each function gets independent compile state so railshot workers
// may populate maps in parallel.
func newGCFrameRootPlan(m *wasm.Module, genericGC bool) *shared.GCModuleFrameRootPlan {
	if !genericGC || m == nil || len(m.Code) == 0 || len(m.Imports) != 0 || len(m.Globals) != 0 || m.Start != nil || m.TableCount() != 0 || len(m.Elements) != 0 || m.TagCount() != 0 {
		return nil
	}
	modulePlan := &shared.GCModuleFrameRootPlan{Functions: make([]*shared.GCFrameRootPlan, len(m.Code))}
	var safepointBase uint32
	for function := range m.Code {
		if bodyHasUnsupportedNativeFrames(m.Code[function].BodyBytes, len(m.Code)) {
			return nil
		}
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return nil
		}
		for _, t := range ft.Params {
			if collectorFrameRefType(m, t) {
				return nil
			}
		}
		for _, t := range ft.Results {
			if collectorFrameRefType(m, t) {
				return nil
			}
		}
		plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase}
		slot, local := 0, uint32(0)
		add := func(t wasm.ValType) bool {
			if collectorFrameRefType(m, t) {
				if len(plan.LocalOffsets) == gcNativeFrameRootLimit || slot > (math.MaxUint32-shared.AMD64FrameHeaderBytes)/8 {
					return false
				}
				plan.LocalIndexes = append(plan.LocalIndexes, local)
				plan.LocalOffsets = append(plan.LocalOffsets, uint32(shared.AMD64FrameHeaderBytes+slot*8))
			}
			if wasm.EqualValType(t, wasm.V128) {
				slot += 2
			} else {
				slot++
			}
			local++
			return true
		}
		for _, t := range ft.Params {
			if !add(t) {
				return nil
			}
		}
		for _, run := range m.Code[function].Locals.Runs {
			for i := uint32(0); i < run.Count; i++ {
				if !add(run.Type) {
					return nil
				}
			}
		}
		liveMasks, err := gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, false)
		if err != nil {
			return nil
		}
		callMasks, err := gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, true)
		if err != nil || (bodyUsesNativeCall(m.Code[function].BodyBytes) && !gcFrameCallABI(ft)) {
			return nil
		}
		if uint64(safepointBase)+uint64(len(liveMasks)) > uint64(shared.GCSafepointIDMax) {
			return nil
		}
		plan.LiveLocalMasks = liveMasks
		plan.LiveCallLocalMasks = callMasks
		modulePlan.Functions[function] = plan
		safepointBase += uint32(len(liveMasks))
	}
	return modulePlan
}

func bodyHasUnsupportedNativeFrames(body []byte, localFunctions int) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
		}
		switch op {
		case 0x06, 0x07, 0x08, 0x09, 0x0a, 0x18, 0x19, 0x1f: // exception-handling control
			return true
		case 0x11, 0x13, 0x14, 0x15: // indirect/call_ref/tail-indirect families
			return true
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return true
		}
		if (op == 0x10 || op == 0x12) && int(imm.Index) >= localFunctions {
			return true
		}
	}
	return false
}

func bodyUsesNativeCall(body []byte) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
		}
		if _, err := wasm.ClassifyInstructionImmediate(r, op); err != nil {
			return true
		}
		if op == 0x10 || op == 0x12 {
			return true
		}
	}
	return false
}

func gcFrameCallABI(ft *wasm.CompType) bool {
	if ft == nil || len(ft.Results) > 2 {
		return false
	}
	gp, fp := 0, 0
	for _, t := range ft.Params {
		switch {
		case wasm.EqualValType(t, wasm.I32), wasm.EqualValType(t, wasm.I64):
			gp++
		case wasm.EqualValType(t, wasm.F32), wasm.EqualValType(t, wasm.F64):
			fp++
		default:
			return false
		}
	}
	if gp > 7 || fp > 8 {
		return false
	}
	for _, t := range ft.Results {
		if !wasm.EqualValType(t, wasm.I32) && !wasm.EqualValType(t, wasm.I64) && !wasm.EqualValType(t, wasm.F32) && !wasm.EqualValType(t, wasm.F64) {
			return false
		}
	}
	return len(ft.Results) != 2 || ((wasm.EqualValType(ft.Results[0], wasm.I32) || wasm.EqualValType(ft.Results[0], wasm.I64)) && (wasm.EqualValType(ft.Results[1], wasm.I32) || wasm.EqualValType(ft.Results[1], wasm.I64)))
}

func collectorFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind != wasm.ValRef {
		return false
	}
	switch t.Ref.Heap.Kind {
	case wasm.HeapAbs:
		switch t.Ref.Heap.Abs {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
			return true
		default:
			return false
		}
	case wasm.HeapDefType:
		def := t.Ref.Heap.Def
		if def == nil || def.Index >= uint32(len(def.Rec.SubTypes)) {
			return true
		}
		kind := def.Rec.SubTypes[def.Index].Comp.Kind
		return kind == wasm.CompStruct || kind == wasm.CompArray
	case wasm.HeapTypeIndex:
		index := t.Ref.Heap.Type.Index
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
}
