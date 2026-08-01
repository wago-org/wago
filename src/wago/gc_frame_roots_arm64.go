//go:build (linux || darwin) && arm64

package wago

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// newGCFrameRootPlan admits the bounded exact arm64 collection product. It
// supports liveness-exact collector locals, compiler-tracked hidden operand
// spills, and direct local call chains. Imports, tables, EH, indirect/reference/
// tail calls, GC globals, and collector/function-reference public signatures
// remain fail-closed until their ownership and frame maps are implemented.
func newGCFrameRootPlan(m *wasm.Module, genericGC bool) *shared.GCModuleFrameRootPlan {
	if !genericGC || m == nil || len(m.Code) == 0 || m.Start != nil || len(m.Imports) != 0 || m.TableCount() != 0 {
		return nil
	}
	for i := range m.Globals {
		if arm64CollectorFrameRefType(m, m.Globals[i].Type.Type) {
			return nil
		}
	}
	module := &shared.GCModuleFrameRootPlan{Functions: make([]*shared.GCFrameRootPlan, len(m.Code))}
	var safepointBase uint32
	for function := range m.Code {
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return nil
		}
		for _, t := range append(append([]wasm.ValType(nil), ft.Params...), ft.Results...) {
			if arm64CollectorFrameRefType(m, t) || arm64FunctionFrameRefType(m, t) {
				return nil
			}
		}
		plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase}
		slot, local := 0, uint32(0)
		add := func(t wasm.ValType) bool {
			if arm64CollectorFrameRefType(m, t) {
				if len(plan.LocalOffsets) == gcNativeFrameRootLimit || slot > (math.MaxUint32-shared.ARM64FrameHeaderBytes)/8 {
					return false
				}
				plan.LocalIndexes = append(plan.LocalIndexes, local)
				plan.LocalOffsets = append(plan.LocalOffsets, uint32(shared.ARM64FrameHeaderBytes+slot*8))
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
		if !arm64GCFrameBodySafe(m, m.Code[function].BodyBytes) {
			return nil
		}
		liveMasks, err := gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, false)
		if err != nil || uint64(safepointBase)+uint64(len(liveMasks)) > uint64(shared.GCSafepointIDMax) {
			return nil
		}
		callMasks, err := gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, true)
		if err != nil {
			return nil
		}
		plan.LiveLocalMasks = liveMasks
		plan.LiveCallLocalMasks = callMasks
		module.Functions[function] = plan
		safepointBase += uint32(len(liveMasks))
	}
	return module
}

func arm64GCFrameBodySafe(m *wasm.Module, body []byte) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false
		}
		switch op {
		case 0x10: // direct local call
			if int(imm.Index) >= len(m.Code) {
				return false
			}
			ft, ok := m.FuncSignature(imm.Index)
			if !ok || !arm64GCFrameCallABI(ft) {
				return false
			}
		case 0x11, 0x12, 0x13, 0x14, 0x15: // indirect, tail, and call_ref
			return false
		case 0x25, 0x26, 0xd2: // tables and ref.func
			return false
		case 0x06, 0x07, 0x08, 0x09, 0x0a, 0x18, 0x19, 0x1f: // EH
			return false
		}
	}
	return true
}

func arm64GCFrameCallABI(ft *wasm.CompType) bool {
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
	if gp > 8 || fp > 8 {
		return false
	}
	for _, t := range ft.Results {
		if !wasm.EqualValType(t, wasm.I32) && !wasm.EqualValType(t, wasm.I64) && !wasm.EqualValType(t, wasm.F32) && !wasm.EqualValType(t, wasm.F64) {
			return false
		}
	}
	return len(ft.Results) != 2 || ((wasm.EqualValType(ft.Results[0], wasm.I32) || wasm.EqualValType(ft.Results[0], wasm.I64)) && (wasm.EqualValType(ft.Results[1], wasm.I32) || wasm.EqualValType(ft.Results[1], wasm.I64)))
}

func arm64FunctionFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind != wasm.ValRef {
		return false
	}
	switch t.Ref.Heap.Kind {
	case wasm.HeapAbs:
		return t.Ref.Heap.Abs == wasm.HeapFunc || t.Ref.Heap.Abs == wasm.HeapNoFunc
	case wasm.HeapTypeIndex:
		ft, ok := m.ResolvedTypeFunc(t.Ref.Heap.Type.Index)
		return ok && ft != nil
	case wasm.HeapDefType:
		def := t.Ref.Heap.Def
		return def != nil && def.Index < uint32(len(def.Rec.SubTypes)) && def.Rec.SubTypes[def.Index].Comp.Kind == wasm.CompFunc
	default:
		return false
	}
}

func arm64CollectorFrameRefType(m *wasm.Module, t wasm.ValType) bool {
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
