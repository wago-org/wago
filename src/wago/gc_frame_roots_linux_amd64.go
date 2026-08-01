//go:build linux && amd64

package wago

import (
	"math"

	railamd64 "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
)

// newGCFrameRootPlan admits bounded local/cross-instance call graphs whose native
// ABI is register-bounded and has at most 64 collector roots
// per function. Each function gets independent compile state so railshot workers
// may populate maps in parallel.
func newGCFrameRootPlan(m *wasm.Module, genericGC bool) *shared.GCModuleFrameRootPlan {
	if !genericGC || m == nil || len(m.Code) == 0 || m.Start != nil {
		return nil
	}
	if !gcFrameTablesSafe(m) {
		return nil
	}
	funcImport := uint32(0)
	for i := range m.Imports {
		switch m.Imports[i].Type.Kind {
		case wasm.ExternFunc:
			ft, ok := m.FuncSignature(funcImport)
			funcImport++
			if !ok || !gcFrameCallABI(m, ft) {
				return nil
			}
		case wasm.ExternGlobal:
			global := m.Imports[i].Type.Global
			if !collectorFrameRefType(m, global.Type) && !frameFunctionRefType(m, global.Type) {
				return nil
			}
		case wasm.ExternTable:
			tableType := wasm.RefVal(m.Imports[i].Type.Table.Ref)
			if !collectorFrameRefType(m, tableType) && !frameFunctionRefType(m, tableType) {
				return nil
			}
		case wasm.ExternMem:
			// Linear-memory imports add no collector roots. Snapshot and linking
			// admission separately prove exact same-domain ownership.
		case wasm.ExternTag:
			// Tag directories are immutable identities; active exception payloads
			// are covered by the function EH root maps below.
		default:
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
		if bodyHasUnsupportedNativeFrames(m, m.Code[function].BodyBytes, importedFunctions, len(m.Code)) {
			return nil
		}
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return nil
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
		if err != nil || (bodyUsesNativeCall(m.Code[function].BodyBytes) && !gcFrameCallABI(m, ft)) {
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

func gcFrameTablesSafe(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	totalFuncs := m.ImportedFuncCount() + len(m.Code)
	functionIndexOK := func(index uint32) bool { return int(index) < totalFuncs }
	tableKinds := make([]uint8, m.TableCount()) // 1=funcref, 2=collector ref
	for tableIndex := 0; tableIndex < m.TableCount(); tableIndex++ {
		tableType, ok := m.TableType(uint32(tableIndex))
		if !ok {
			return false
		}
		typ := wasm.RefVal(tableType.Ref)
		switch {
		case frameFunctionRefType(m, typ):
			tableKinds[tableIndex] = 1
		case collectorFrameRefType(m, typ):
			tableKinds[tableIndex] = 2
		default:
			return false
		}
	}
	for i := range m.Tables {
		if m.Tables[i].Init == nil {
			continue
		}
		ee, err := wasm.ParseElementExpr(*m.Tables[i].Init)
		kind := tableKinds[m.ImportedTableCount()+i]
		if err != nil || ee.HasGlobal || (!ee.Null && (kind != 1 || !functionIndexOK(ee.FuncIndex))) {
			return false
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive && e.Mode.Kind != wasm.ElemPassive && e.Mode.Kind != wasm.ElemDeclarative {
			return false
		}
		kind := uint8(0)
		if e.Mode.Kind == wasm.ElemActive {
			if int(e.Mode.Table) >= len(tableKinds) {
				return false
			}
			kind = tableKinds[e.Mode.Table]
		} else if e.Kind.Kind == wasm.ElemFuncs || frameFunctionRefType(m, wasm.RefVal(e.Kind.Ref)) {
			kind = 1
		} else if collectorFrameRefType(m, wasm.RefVal(e.Kind.Ref)) {
			kind = 2
		} else {
			return false
		}
		if e.Kind.Kind == wasm.ElemFuncs {
			if kind != 1 {
				return false
			}
			for _, index := range e.Kind.Funcs {
				if !functionIndexOK(uint32(index)) {
					return false
				}
			}
			continue
		}
		for _, expr := range e.Kind.Exprs {
			if kind == 2 && e.Kind.Ref.Heap.Kind == wasm.HeapAbs && e.Kind.Ref.Heap.Abs == wasm.HeapI31 {
				// Validation and compileElemValues already proved each expression is
				// an exact immediate i31; it carries no collector object root.
				continue
			}
			ee, err := wasm.ParseElementExpr(expr)
			if err != nil || ee.HasGlobal || (!ee.Null && (kind != 1 || !functionIndexOK(ee.FuncIndex))) {
				return false
			}
		}
	}
	return true
}

func bodyHasUnsupportedNativeFrames(m *wasm.Module, body []byte, importedFunctions, localFunctions int) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
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
			if !ok || !gcFrameCallABI(m, ft) {
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

func gcFrameCallABI(m *wasm.Module, ft *wasm.CompType) bool {
	if ft == nil || len(ft.Results) > 2 {
		return false
	}
	gp, fp := 0, 0
	for _, t := range ft.Params {
		switch {
		case wasm.EqualValType(t, wasm.I32), wasm.EqualValType(t, wasm.I64), collectorFrameRefType(m, t), frameFunctionRefType(m, t):
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
		if !wasm.EqualValType(t, wasm.I32) && !wasm.EqualValType(t, wasm.I64) && !wasm.EqualValType(t, wasm.F32) && !wasm.EqualValType(t, wasm.F64) && !collectorFrameRefType(m, t) && !frameFunctionRefType(m, t) {
			return false
		}
	}
	integerResult := func(t wasm.ValType) bool {
		return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64) || collectorFrameRefType(m, t) || frameFunctionRefType(m, t)
	}
	return len(ft.Results) != 2 || (integerResult(ft.Results[0]) && integerResult(ft.Results[1]))
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
