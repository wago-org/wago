//go:build (linux || darwin) && arm64

package wago

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// newGCFrameRootPlan admits the first exact arm64 collection slice. It is
// deliberately narrower than amd64: no wasm calls, tables, imports, EH, GC
// globals, or collector-reference parameters/results. Allocating instructions
// are limited to struct.new_default, whose result must be immediately stored in
// a collector local or dropped. Collector locals cannot be read before the last
// textual allocation. These restrictions prove that every collector reference
// live at an allocating helper is in a mapped local slot; constructor arguments
// and hidden operand roots are therefore absent in this initial arm64 product.
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
		if !arm64GCFrameBodySafe(m, m.Code[function].BodyBytes, plan.LocalIndexes) {
			return nil
		}
		liveMasks, err := gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, false)
		if err != nil || uint64(safepointBase)+uint64(len(liveMasks)) > uint64(shared.GCSafepointIDMax) {
			return nil
		}
		plan.LiveLocalMasks = liveMasks
		module.Functions[function] = plan
		safepointBase += uint32(len(liveMasks))
	}
	return module
}

type arm64GCFrameOp struct {
	op       byte
	kind     wasm.InstrKind
	sub      uint32
	index    uint32
	alloc    bool
	localSet bool
	drop     bool
}

func arm64GCFrameBodySafe(m *wasm.Module, body []byte, collectorLocals []uint32) bool {
	localRoot := make(map[uint32]bool, len(collectorLocals))
	for _, index := range collectorLocals {
		localRoot[index] = true
	}
	r := wasm.NewReader(body)
	ops := make([]arm64GCFrameOp, 0, len(body)/2+1)
	lastAllocation := -1
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false
		}
		record := arm64GCFrameOp{op: op, kind: imm.Kind, sub: imm.Subopcode, index: imm.Index, localSet: imm.Kind == wasm.InstrLocalSet, drop: op == 0x1a}
		switch {
		case op == 0xfb && imm.Subopcode == 1: // struct.new_default
			record.alloc = true
			lastAllocation = len(ops)
		case op == 0xfb && imm.Subopcode == 2: // struct.get: only numeric fields
			st, ok := arm64SnapshotStructType(m, imm.Index)
			if !ok || imm.Index2 >= uint32(len(st.Comp.Fields)) || st.Comp.Fields[imm.Index2].Storage.Val.Kind == wasm.ValRef {
				return false
			}
		case op == 0xfb:
			return false
		case op == 0x10 || op == 0x11 || op == 0x12 || op == 0x13 || op == 0x14 || op == 0x15:
			return false
		case op == 0xd0 || op == 0xd2 || op == 0x25 || op == 0x26:
			return false
		case op == 0x06 || op == 0x07 || op == 0x08 || op == 0x09 || op == 0x0a || op == 0x18 || op == 0x19 || op == 0x1f:
			return false
		}
		ops = append(ops, record)
	}
	if lastAllocation < 0 {
		return true
	}
	for i, record := range ops {
		if record.alloc {
			if i+1 >= len(ops) || !(ops[i+1].drop || (ops[i+1].localSet && localRoot[ops[i+1].index])) {
				return false
			}
		}
		if i < lastAllocation && record.kind == wasm.InstrLocalGet && localRoot[record.index] {
			return false
		}
	}
	return true
}

func arm64SnapshotStructType(m *wasm.Module, typeIndex uint32) (wasm.SubType, bool) {
	index := typeIndex
	for _, group := range m.Types {
		if index < uint32(len(group.SubTypes)) {
			sub := group.SubTypes[index]
			return sub, sub.Comp.Kind == wasm.CompStruct
		}
		index -= uint32(len(group.SubTypes))
	}
	return wasm.SubType{}, false
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
