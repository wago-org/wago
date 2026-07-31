//go:build linux && amd64

package wago

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// newGCFrameRootPlan admits the first exact native-root slice: one import-free
// local function, no table/element/start/EH/call frames, and at most 64
// collector-reference locals. The amd64 backend additionally proves that every
// allocating helper has no operand below its declared arguments.
func newGCFrameRootPlan(m *wasm.Module, genericGC bool) *shared.GCFrameRootPlan {
	if !genericGC || m == nil || len(m.Code) != 1 || len(m.Imports) != 0 || len(m.Globals) != 0 || m.Start != nil || m.TableCount() != 0 || len(m.Elements) != 0 || m.TagCount() != 0 || bodyHasUnsupportedNativeFrames(m.Code[0].BodyBytes) {
		return nil
	}
	ft, ok := m.LocalFuncType(0)
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
	plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true}
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
	for _, run := range m.Code[0].Locals.Runs {
		for i := uint32(0); i < run.Count; i++ {
			if !add(run.Type) {
				return nil
			}
		}
	}
	liveMasks, err := gcFrameLocalLiveness(m.Code[0].BodyBytes, plan.LocalIndexes, false)
	if err != nil {
		return nil
	}
	callMasks, err := gcFrameLocalLiveness(m.Code[0].BodyBytes, plan.LocalIndexes, true)
	if err != nil || (bodyUsesSelfNativeCall(m.Code[0].BodyBytes) && !gcFrameSelfCallABI(ft)) {
		return nil
	}
	plan.LiveLocalMasks = liveMasks
	plan.LiveCallLocalMasks = callMasks
	return plan
}

func bodyHasUnsupportedNativeFrames(body []byte) bool {
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
		if (op == 0x10 || op == 0x12) && imm.Index != 0 { // direct and tail self-calls only
			return true
		}
	}
	return false
}

func bodyUsesSelfNativeCall(body []byte) bool {
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

func gcFrameSelfCallABI(ft *wasm.CompType) bool {
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
			return true // fail safe: unknown defined refs are never omitted
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
		return true // validated modules should not reach this; do not omit a root
	default:
		return true
	}
}
