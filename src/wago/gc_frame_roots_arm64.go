//go:build (linux || darwin) && arm64

package wago

import (
	"math"

	railarm64 "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// newGCFrameRootPlan admits the bounded exact arm64 collection product. It
// supports liveness-exact collector locals, hidden operand spills, direct and
// recursive calls, direct host re-entry, same-domain foreign calls, mutable/shared
// GC globals and collector tables, polymorphic call_indirect, and local or
// same-domain foreign call_ref. Unsupported tail-reference ownership remains
// fail-closed.
func newGCFrameRootPlan(m *wasm.Module, genericGC bool) *shared.GCModuleFrameRootPlan {
	if !genericGC || m == nil || len(m.Code) == 0 || m.Start != nil {
		return nil
	}
	tablesSafe, collectorTable := arm64GCFrameTablesSafe(m)
	if !tablesSafe {
		return nil
	}
	for i := range m.Globals {
		if arm64FunctionFrameRefType(m, m.Globals[i].Type.Type) {
			return nil
		}
	}
	funcImport := uint32(0)
	for i := range m.Imports {
		switch m.Imports[i].Type.Kind {
		case wasm.ExternFunc:
			ft, ok := m.FuncSignature(funcImport)
			funcImport++
			if !ok || !arm64GCFrameCallABI(m, ft) {
				return nil
			}
		case wasm.ExternGlobal:
			global := m.Imports[i].Type.Global
			if !arm64CollectorFrameRefType(m, global.Type) || arm64FunctionFrameRefType(m, global.Type) {
				return nil
			}
		case wasm.ExternTable:
			if !arm64CollectorFrameRefType(m, wasm.RefVal(m.Imports[i].Type.Table.Ref)) {
				return nil
			}
		case wasm.ExternMem:
			// Linear-memory imports add no collector roots. Snapshot and linking
			// admission separately prove exact same-domain ownership.
		case wasm.ExternTag:
			// Tags carry no independently rooted instance storage.
		default:
			return nil
		}
	}
	ehMaps, err := railarm64.BuildExceptionRootMaps(m)
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
	module := &shared.GCModuleFrameRootPlan{Functions: make([]*shared.GCFrameRootPlan, len(m.Code))}
	var safepointBase uint32
	for function := range m.Code {
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return nil
		}
		for _, t := range append(append([]wasm.ValType(nil), ft.Params...), ft.Results...) {
			if arm64FunctionFrameRefType(m, t) {
				return nil
			}
		}
		plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase, FixedOffsets: fixedRoots[function]}
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
		if !arm64GCFrameBodySafe(m, m.Code[function].BodyBytes, collectorTable) {
			return nil
		}
		var liveMasks, callMasks []uint64
		if arm64BodyUsesEH(m.Code[function].BodyBytes) {
			liveMasks, callMasks, err = arm64GCFrameConservativeMasks(m.Code[function].BodyBytes, len(plan.LocalIndexes))
		} else {
			liveMasks, err = gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, false)
			if err == nil {
				callMasks, err = gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, true)
			}
		}
		if err != nil || uint64(safepointBase)+uint64(len(liveMasks)) > uint64(shared.GCSafepointIDMax) {
			return nil
		}
		plan.LiveLocalMasks = liveMasks
		plan.LiveCallLocalMasks = callMasks
		module.Functions[function] = plan
		safepointBase += uint32(len(liveMasks))
	}
	return module
}

func arm64GCFrameTablesSafe(m *wasm.Module) (safe, collector bool) {
	if m.TableCount() == 0 {
		for i := range m.Elements {
			e := &m.Elements[i]
			if e.Mode.Kind != wasm.ElemDeclarative || e.Kind.Kind != wasm.ElemFuncs {
				return false, false
			}
			for _, idx := range e.Kind.Funcs {
				if int(idx) >= m.ImportedFuncCount()+len(m.Code) {
					return false, false
				}
			}
		}
		return true, false
	}
	collectorTables := true
	for tableIndex := 0; tableIndex < m.TableCount(); tableIndex++ {
		tableType, ok := m.TableType(uint32(tableIndex))
		if !ok || !arm64CollectorFrameRefType(m, wasm.RefVal(tableType.Ref)) {
			collectorTables = false
			break
		}
	}
	if collectorTables {
		for i := range m.Tables {
			if m.Tables[i].Init == nil {
				continue
			}
			ee, err := wasm.ParseElementExpr(*m.Tables[i].Init)
			if err != nil || ee.HasGlobal || !ee.Null {
				return false, false
			}
		}
		for i := range m.Elements {
			e := &m.Elements[i]
			if e.Mode.Kind != wasm.ElemActive || int(e.Mode.Table) >= m.TableCount() || e.Kind.Kind == wasm.ElemFuncs {
				return false, false
			}
			for _, expr := range e.Kind.Exprs {
				ee, err := wasm.ParseElementExpr(expr)
				if err != nil || ee.HasGlobal || !ee.Null {
					return false, false
				}
			}
		}
		return true, true
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
	collectorTable := arm64CollectorFrameRefType(m, wasm.RefVal(ref))
	if !functionTable && !collectorTable {
		return false, false
	}
	if init := m.Tables[0].Init; init != nil {
		ee, err := wasm.ParseElementExpr(*init)
		if err != nil || ee.HasGlobal || (functionTable && !ee.Null && int(ee.FuncIndex) >= m.ImportedFuncCount()+len(m.Code)) || (collectorTable && !ee.Null) {
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
				if int(idx) >= m.ImportedFuncCount()+len(m.Code) {
					return false, false
				}
			}
		default:
			for _, expr := range e.Kind.Exprs {
				ee, err := wasm.ParseElementExpr(expr)
				if err != nil || ee.HasGlobal || (!ee.Null && int(ee.FuncIndex) >= m.ImportedFuncCount()+len(m.Code)) {
					return false, false
				}
			}
		}
	}
	return true, collectorTable
}

func arm64GCFrameBodySafe(m *wasm.Module, body []byte, collectorTable bool) bool {
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
		case 0x10: // direct local or imported call
			if int(imm.Index) >= m.ImportedFuncCount()+len(m.Code) {
				return false
			}
			ft, ok := m.FuncSignature(imm.Index)
			if !ok || !arm64GCFrameCallABI(m, ft) {
				return false
			}
		case 0x11: // one private immutable function table
			if imm.Index2 != 0 || collectorTable {
				return false
			}
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || !arm64GCFrameCallABI(m, ft) {
				return false
			}
		case 0x12: // direct tail call; the caller frame is discarded
			if int(imm.Index) >= m.ImportedFuncCount()+len(m.Code) {
				return false
			}
			ft, ok := m.FuncSignature(imm.Index)
			if !ok || !arm64GCFrameCallABI(m, ft) || funcTypeSlotsForRoots(ft.Params) > abi.TailArgsSlots {
				return false
			}
		case 0x13: // monomorphic private-table tail call
			if imm.Index2 != 0 || collectorTable || !arm64GCFunctionTableMonomorphic(m) {
				return false
			}
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || funcTypeSlotsForRoots(ft.Params) > abi.TailArgsSlots {
				return false
			}
		case 0x14: // local or same-domain foreign typed function reference
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || !arm64GCFrameCallABI(m, ft) {
				return false
			}
		case 0x15: // local or exact imported typed-reference tail call
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || funcTypeSlotsForRoots(ft.Params) > abi.TailArgsSlots {
				return false
			}
		case 0x26: // table.set invalidates local function-target identity
			if !collectorTable {
				return false
			}
		case 0xd2: // ref.func may name any validated local or imported function
			if collectorTable || int(imm.Index) >= m.ImportedFuncCount()+len(m.Code) {
				return false
			}
		case 0xfc:
			if !collectorTable {
				switch imm.Subopcode {
				case 12, 14, 15, 17: // table.init/copy/grow/fill
					return false
				}
			}
		case 0x06, 0x07, 0x09, 0x18, 0x19: // legacy EH forms are not lowered
			return false
		}
	}
	return true
}

func arm64BodyUsesEH(body []byte) bool {
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

func arm64GCFrameConservativeMasks(body []byte, localRoots int) (allocations, calls []uint64, err error) {
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

func arm64GCFunctionTableMonomorphic(m *wasm.Module) bool {
	if m == nil || m.ImportedTableCount() != 0 || len(m.Tables) != 1 {
		return false
	}
	target := int64(-1)
	add := func(index uint32) bool {
		if int(index) < m.ImportedFuncCount() || int(index)-m.ImportedFuncCount() >= len(m.Code) {
			return false
		}
		if target < 0 {
			target = int64(index)
			return true
		}
		return target == int64(index)
	}
	if init := m.Tables[0].Init; init != nil {
		ee, err := wasm.ParseElementExpr(*init)
		if err != nil || ee.HasGlobal || (!ee.Null && !add(ee.FuncIndex)) {
			return false
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive || e.Mode.Table != 0 {
			continue
		}
		if e.Kind.Kind == wasm.ElemFuncs {
			for _, index := range e.Kind.Funcs {
				if !add(uint32(index)) {
					return false
				}
			}
			continue
		}
		for _, expr := range e.Kind.Exprs {
			ee, err := wasm.ParseElementExpr(expr)
			if err != nil || ee.HasGlobal || (!ee.Null && !add(ee.FuncIndex)) {
				return false
			}
		}
	}
	return target >= 0
}

func funcTypeSlotsForRoots(ts []wasm.ValType) int {
	n := 0
	for _, t := range ts {
		if wasm.EqualValType(t, wasm.V128) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func arm64GCFrameCallRefABI(ft *wasm.CompType) bool {
	if ft == nil || len(ft.Results) > 2 || len(ft.Params) > 7 {
		return false
	}
	for _, t := range append(append([]wasm.ValType(nil), ft.Params...), ft.Results...) {
		if !wasm.EqualValType(t, wasm.I32) && !wasm.EqualValType(t, wasm.I64) {
			return false
		}
	}
	return true
}

func arm64GCFrameCallABI(m *wasm.Module, ft *wasm.CompType) bool {
	if ft == nil || len(ft.Results) > 2 {
		return false
	}
	gp, fp := 0, 0
	for _, t := range ft.Params {
		switch {
		case wasm.EqualValType(t, wasm.I32), wasm.EqualValType(t, wasm.I64), arm64CollectorFrameRefType(m, t):
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
	integerResult := func(t wasm.ValType) bool {
		return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64) || arm64CollectorFrameRefType(m, t)
	}
	for _, t := range ft.Results {
		if !integerResult(t) && !wasm.EqualValType(t, wasm.F32) && !wasm.EqualValType(t, wasm.F64) {
			return false
		}
	}
	return len(ft.Results) != 2 || (integerResult(ft.Results[0]) && integerResult(ft.Results[1]))
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
