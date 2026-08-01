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
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != wasm.ExternFunc {
			return nil
		}
		ft, ok := m.FuncSignature(uint32(i))
		if !ok || !arm64GCFrameCallABI(m, ft) {
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
			if arm64FunctionFrameRefType(m, t) {
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
		if !arm64GCFrameBodySafe(m, m.Code[function].BodyBytes, collectorTable) {
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

func arm64GCFrameTablesSafe(m *wasm.Module) (safe, collector bool) {
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
	collectorTable := arm64CollectorFrameRefType(m, wasm.RefVal(ref))
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
		case 0x11: // one private immutable monomorphic function table
			if collectorTable || !arm64GCFunctionTableMonomorphic(m) {
				return false
			}
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || !arm64GCFrameCallABI(m, ft) {
				return false
			}
		case 0x14: // local typed function reference
			if m.ImportedFuncCount() != 0 {
				return false
			}
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || !arm64GCFrameCallRefABI(ft) {
				return false
			}
		case 0x12, 0x13, 0x15: // tail calls are not lowered by arm64 yet
			return false
		case 0x26: // table.set invalidates local function-target identity
			if !collectorTable {
				return false
			}
		case 0xd2: // ref.func is safe only for a local function identity
			if collectorTable || int(imm.Index) < m.ImportedFuncCount() || int(imm.Index)-m.ImportedFuncCount() >= len(m.Code) {
				return false
			}
		case 0xfc:
			if !collectorTable {
				switch imm.Subopcode {
				case 12, 14, 15, 17: // table.init/copy/grow/fill
					return false
				}
			}
		case 0x06, 0x07, 0x08, 0x09, 0x0a, 0x18, 0x19, 0x1f: // EH
			return false
		}
	}
	return true
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
