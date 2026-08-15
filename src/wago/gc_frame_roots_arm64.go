//go:build (linux || darwin) && arm64 && !wago_precompiled

package wago

import (
	"fmt"
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
	if !genericGC {
		return nil
	}
	reject := func(format string, args ...any) *shared.GCModuleFrameRootPlan {
		return &shared.GCModuleFrameRootPlan{Diagnostic: fmt.Sprintf(format, args...)}
	}
	if m == nil || len(m.Code) == 0 {
		return reject("generic GC module has no local function bodies")
	}
	if m.Start != nil && int(*m.Start) < m.ImportedFuncCount() {
		return reject("imported start function has an unknown host ownership graph")
	}
	if !arm64GCFrameTablesSafe(m) {
		return reject("table or element ownership is outside the exact native-root model")
	}
	funcImport := uint32(0)
	for i := range m.Imports {
		switch m.Imports[i].Type.Kind {
		case wasm.ExternFunc:
			ft, ok := m.FuncSignature(funcImport)
			funcImport++
			if !ok || !arm64GCFrameCallABI(m, ft) {
				return reject("function import %d exceeds the exact native call ABI", funcImport-1)
			}
		case wasm.ExternGlobal:
			global := m.Imports[i].Type.GlobalType()
			if !arm64CollectorFrameRefType(m, global.Type) && !arm64FunctionFrameRefType(m, global.Type) {
				return reject("global import %d has an unsupported reference ownership shape", i)
			}
		case wasm.ExternTable:
			tableType := wasm.RefVal(m.Imports[i].Type.TableType().Ref)
			if !arm64CollectorFrameRefType(m, tableType) && !arm64FunctionFrameRefType(m, tableType) {
				return reject("table import %d has an unsupported reference ownership shape", i)
			}
		case wasm.ExternMem:
			// Linear-memory imports add no collector roots. Linking admission
			// separately proves exact same-domain ownership.
		case wasm.ExternTag:
			// Tags carry no independently rooted instance storage.
		default:
			return reject("import %d has unsupported external kind %d", i, m.Imports[i].Type.Kind)
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	ehMaps, err := railarm64.BuildExceptionRootMaps(m)
	if err != nil {
		return reject("exception root maps: %v", err)
	}
	fixedRoots := make([][]uint32, len(m.Code))
	for i := range ehMaps {
		if int(ehMaps[i].LocalFunction) >= len(fixedRoots) {
			return reject("exception root map function %d is out of range", ehMaps[i].LocalFunction)
		}
		for _, slot := range ehMaps[i].Slots {
			if slot.Kind == nativeabi.RootGCRef {
				fixedRoots[ehMaps[i].LocalFunction] = append(fixedRoots[ehMaps[i].LocalFunction], slot.Offset)
			}
		}
	}
	module := &shared.GCModuleFrameRootPlan{Functions: make([]*shared.GCFrameRootPlan, len(m.Code))}
	var safepointBase uint32
functions:
	for function := range m.Code {
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return reject("function %d has no validated signature", function)
		}
		plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase, FixedOffsets: fixedRoots[function]}
		mayCollect := gcFrameBodyMayCollectWithClassifier(m.Code[function].BodyBytes, &classifier)
		slot, local := 0, uint32(0)
		add := func(t wasm.ValType) bool {
			if arm64CollectorFrameRefType(m, t) {
				if len(plan.LocalOffsets) == shared.GCFrameRootLimit || slot > (math.MaxUint32-shared.ARM64FrameHeaderBytes)/8 {
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
				if !mayCollect {
					continue functions
				}
				return reject("function %d exceeds %d collector roots or the frame-offset bound", function, shared.GCFrameRootLimit)
			}
		}
		for _, run := range m.Code[function].Locals.Runs {
			for i := uint32(0); i < run.Count; i++ {
				if !add(run.Type) {
					if !mayCollect {
						continue functions
					}
					return reject("function %d exceeds %d collector roots or the frame-offset bound", function, shared.GCFrameRootLimit)
				}
			}
		}
		if !arm64GCFrameBodySafe(m, m.Code[function].BodyBytes, &classifier) {
			return reject("function %d contains an unsupported native call or frame shape", function)
		}
		var liveMasks, callMasks []uint64
		var maskExtra gcFrameLivenessExtra
		if arm64BodyUsesEH(m.Code[function].BodyBytes, &classifier) {
			liveMasks, callMasks, err = arm64GCFrameConservativeMasks(m.Code[function].BodyBytes, len(plan.LocalIndexes), &maskExtra, &classifier)
		} else {
			liveMasks, err = gcFrameLocalLivenessWithClassifier(m.Code[function].BodyBytes, plan.LocalIndexes, &callMasks, &maskExtra, &classifier)
		}
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if uint64(safepointBase)+uint64(len(liveMasks)) > uint64(shared.GCSafepointIDMax) {
			return reject("function %d exceeds the dense safepoint ID bound", function)
		}
		plan.LiveLocalMasks = liveMasks
		plan.LiveCallLocalMasks = callMasks
		plan.LiveMaskExtraWords = maskExtra.words
		module.Functions[function] = plan
		safepointBase += uint32(len(liveMasks))
	}
	return module
}

func arm64GCFrameTablesSafe(m *wasm.Module) bool {
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
		case arm64FunctionFrameRefType(m, typ):
			tableKinds[tableIndex] = 1
		case arm64CollectorFrameRefType(m, typ):
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
		} else if e.Kind.Kind == wasm.ElemFuncs || arm64FunctionFrameRefType(m, wasm.RefVal(e.Kind.Ref)) {
			kind = 1
		} else if arm64CollectorFrameRefType(m, wasm.RefVal(e.Kind.Ref)) {
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
			if kind == 2 && e.Kind.Ref.Heap().Kind() == wasm.HeapAbs && e.Kind.Ref.Heap().Abs() == wasm.HeapI31 {
				// Exact i31 element expressions are immediate or immutable-global
				// values, not independent object roots.
				continue
			}
			ee, err := wasm.ParseElementExpr(expr)
			if err != nil {
				if kind == 2 && gcFrameCollectorElementExprSafe(expr) {
					continue
				}
				return false
			}
			if ee.HasGlobal {
				gt, ok := m.GlobalTypeByIndex(ee.GlobalIndex)
				if !ok || gt.Mutable || !arm64CollectorFrameRefType(m, gt.Type) {
					return false
				}
				continue
			}
			if !ee.Null && (kind != 1 || !functionIndexOK(ee.FuncIndex)) {
				return false
			}
		}
	}
	return true
}

func arm64GCFrameBodySafe(m *wasm.Module, body []byte, classifier *wasm.ModuleInstructionClassifier) bool {
	r := wasm.NewReader(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
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
		case 0x11: // dynamically validated function table
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
		case 0x13: // tail-indirect remains bounded to its backend-admitted shape
			if imm.Index2 != 0 || !arm64GCFunctionTableMonomorphic(m) {
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
		case 0xd2: // ref.func may name any validated local or imported function
			if int(imm.Index) >= m.ImportedFuncCount()+len(m.Code) {
				return false
			}
		case 0x06, 0x07, 0x09, 0x18, 0x19: // legacy EH forms are not lowered
			return false
		}
	}
	return true
}

func arm64BodyUsesEH(body []byte, classifier *wasm.ModuleInstructionClassifier) bool {
	r := wasm.NewReader(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
		}
		switch op {
		case 0x06, 0x07, 0x08, 0x09, 0x0a, 0x18, 0x19, 0x1f:
			return true
		}
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return true
		}
	}
	return false
}

func arm64GCFrameConservativeMasks(body []byte, localRoots int, extra *gcFrameLivenessExtra, classifier *wasm.ModuleInstructionClassifier) (allocations, calls []uint64, err error) {
	return gcFrameAllLiveMasksWithClassifier(body, localRoots, extra, classifier)
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
		case wasm.EqualValType(t, wasm.I32), wasm.EqualValType(t, wasm.I64), arm64CollectorFrameRefType(m, t), arm64FunctionFrameRefType(m, t):
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
		return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64) || arm64CollectorFrameRefType(m, t) || arm64FunctionFrameRefType(m, t)
	}
	for _, t := range ft.Results {
		if !integerResult(t) && !wasm.EqualValType(t, wasm.F32) && !wasm.EqualValType(t, wasm.F64) {
			return false
		}
	}
	return len(ft.Results) != 2 || (integerResult(ft.Results[0]) && integerResult(ft.Results[1]))
}

func arm64FunctionFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind() != wasm.ValRef {
		return false
	}
	heap := t.Ref().Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		return heap.Abs() == wasm.HeapFunc || heap.Abs() == wasm.HeapNoFunc
	case wasm.HeapTypeIndex:
		var ft wasm.CompType
		return m.ResolveTypeFunc(heap.Type().Index, &ft)
	case wasm.HeapDefType:
		kind, valid := heap.DefCompKind()
		return valid && kind == wasm.CompFunc
	default:
		return false
	}
}

func arm64CollectorFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind() != wasm.ValRef {
		return false
	}
	heap := t.Ref().Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
			return true
		default:
			return false
		}
	case wasm.HeapDefType:
		kind, valid := heap.DefCompKind()
		if !valid {
			return true
		}
		return kind == wasm.CompStruct || kind == wasm.CompArray
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
}
