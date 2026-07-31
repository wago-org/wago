//go:build linux && amd64

package wago

import (
	"math"

	railamd64 "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
)

// newGCFrameRootPlan admits import/global/start/table/element/tag/EH-free local
// call graphs with numeric native-call signatures and at most 64 collector roots
// per function. Each function gets independent compile state so railshot workers
// may populate maps in parallel.
func newGCFrameRootPlan(m *wasm.Module, genericGC bool) *shared.GCModuleFrameRootPlan {
	if !genericGC || m == nil || len(m.Code) == 0 || m.Start != nil {
		return nil
	}
	tablesSafe, collectorTable := gcFrameTablesSafe(m)
	if !tablesSafe {
		return nil
	}
	for i := range m.Globals {
		if frameFunctionRefType(m, m.Globals[i].Type.Type) {
			return nil
		}
	}
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != wasm.ExternFunc {
			return nil
		}
		ft, ok := m.FuncSignature(uint32(i))
		if !ok || !gcFrameCallABI(ft) {
			return nil
		}
	}
	importedFunctions := m.ImportedFuncCount()
	ehMaps, err := railamd64.BuildExceptionRootMaps(m)
	if err != nil {
		return nil
	}
	fixedRoots := make([][]uint32, len(m.Code))
	for i := range ehMaps {
		if int(ehMaps[i].LocalFunction) >= len(fixedRoots) {
			return nil
		}
		for _, slot := range ehMaps[i].Slots {
			if slot.Kind == nativeabi.RootGCRef {
				fixedRoots[ehMaps[i].LocalFunction] = append(fixedRoots[ehMaps[i].LocalFunction], slot.Offset)
			}
		}
	}
	modulePlan := &shared.GCModuleFrameRootPlan{Functions: make([]*shared.GCFrameRootPlan, len(m.Code))}
	var safepointBase uint32
	for function := range m.Code {
		if bodyHasUnsupportedNativeFrames(m, m.Code[function].BodyBytes, importedFunctions, len(m.Code), collectorTable) {
			return nil
		}
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return nil
		}
		for _, t := range ft.Params {
			if collectorFrameRefType(m, t) || frameFunctionRefType(m, t) {
				return nil
			}
		}
		for _, t := range ft.Results {
			if collectorFrameRefType(m, t) || frameFunctionRefType(m, t) {
				return nil
			}
		}
		plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase, FixedOffsets: fixedRoots[function]}
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
		var liveMasks, callMasks []uint64
		var err error
		if bodyUsesEH(m.Code[function].BodyBytes) {
			liveMasks, callMasks, err = gcFrameConservativeMasks(m.Code[function].BodyBytes, len(plan.LocalIndexes))
		} else {
			liveMasks, err = gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, false)
			if err == nil {
				callMasks, err = gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, true)
			}
		}
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

func gcFrameTablesSafe(m *wasm.Module) (safe, collector bool) {
	if m.TableCount() == 0 {
		for i := range m.Elements {
			e := &m.Elements[i]
			if e.Mode.Kind != wasm.ElemDeclarative || e.Kind.Kind != wasm.ElemFuncs {
				return false, false
			}
			for _, idx := range e.Kind.Funcs {
				if int(idx) < m.ImportedFuncCount() || int(idx)-m.ImportedFuncCount() >= len(m.Code) {
					return false, false
				}
			}
		}
		return true, false
	}
	if m.ImportedTableCount() != 0 || len(m.Tables) != 1 {
		return false, false
	}
	for _, export := range m.Exports {
		if export.Index.Kind == wasm.ExternTable {
			return false, false
		}
	}
	ref := m.Tables[0].Type.Ref
	functionTable := ref.Heap.Kind == wasm.HeapAbs && (ref.Heap.Abs == wasm.HeapFunc || ref.Heap.Abs == wasm.HeapNoFunc)
	collectorTable := collectorFrameRefType(m, wasm.RefVal(ref))
	if !functionTable && !collectorTable {
		return false, false
	}
	if init := m.Tables[0].Init; init != nil {
		ee, err := wasm.ParseElementExpr(*init)
		if err != nil || ee.HasGlobal || (functionTable && !ee.Null && (int(ee.FuncIndex) < m.ImportedFuncCount() || int(ee.FuncIndex)-m.ImportedFuncCount() >= len(m.Code))) || (collectorTable && !ee.Null) {
			return false, false
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive || e.Mode.Table != 0 {
			return false, false
		}
		if collectorTable {
			for _, expr := range e.Kind.Exprs {
				ee, err := wasm.ParseElementExpr(expr)
				if err != nil || ee.HasGlobal || !ee.Null {
					return false, false
				}
			}
			if e.Kind.Kind == wasm.ElemFuncs {
				return false, false
			}
			continue
		}
		switch e.Kind.Kind {
		case wasm.ElemFuncs:
			for _, idx := range e.Kind.Funcs {
				if int(idx) < m.ImportedFuncCount() || int(idx)-m.ImportedFuncCount() >= len(m.Code) {
					return false, false
				}
			}
		default:
			for _, expr := range e.Kind.Exprs {
				ee, err := wasm.ParseElementExpr(expr)
				if err != nil || ee.HasGlobal || (!ee.Null && (int(ee.FuncIndex) < m.ImportedFuncCount() || int(ee.FuncIndex)-m.ImportedFuncCount() >= len(m.Code))) {
					return false, false
				}
			}
		}
	}
	return true, collectorTable
}

func bodyHasUnsupportedNativeFrames(m *wasm.Module, body []byte, importedFunctions, localFunctions int, collectorTable bool) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
		}
		switch op {
		case 0x26: // table.set invalidates local function-target identity
			if !collectorTable {
				return true
			}
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return true
		}
		if (op == 0x10 || op == 0x12) && int(imm.Index) >= importedFunctions+localFunctions {
			return true
		}
		if op == 0x14 || op == 0x15 {
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || !gcFrameCallABI(ft) {
				return true
			}
		}
		if op == 0xfc && !collectorTable {
			switch imm.Subopcode {
			case 12, 14, 15, 17: // table.init/copy/grow/fill
				return true
			}
		}
	}
	return false
}

func bodyUsesEH(body []byte) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
		}
		switch op {
		case 0x06, 0x07, 0x08, 0x09, 0x0a, 0x18, 0x19, 0x1f:
			return true
		}
		if _, err := wasm.ClassifyInstructionImmediate(r, op); err != nil {
			return true
		}
	}
	return false
}

func gcFrameConservativeMasks(body []byte, localRoots int) (allocations, calls []uint64, err error) {
	var mask uint64
	if localRoots >= 64 {
		mask = ^uint64(0)
	} else if localRoots > 0 {
		mask = uint64(1)<<uint(localRoots) - 1
	}
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, readErr := r.Byte()
		if readErr != nil {
			return nil, nil, readErr
		}
		imm, readErr := wasm.ClassifyInstructionImmediate(r, op)
		if readErr != nil {
			return nil, nil, readErr
		}
		if op == 0xfb {
			switch imm.Subopcode {
			case 0, 1, 6, 7, 8, 9, 10:
				allocations = append(allocations, mask)
			}
		}
		if op == 0x10 || op == 0x11 || op == 0x14 {
			calls = append(calls, mask)
		}
	}
	return allocations, calls, nil
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
		if op == 0x10 || op == 0x12 || op == 0x14 || op == 0x15 {
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

func frameFunctionRefType(m *wasm.Module, t wasm.ValType) bool {
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
