//go:build linux && amd64 && !wago_precompiled

package wago

import (
	"fmt"
	"math"

	railamd64 "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
)

// newGCFrameRootPlan admits bounded local/cross-instance call graphs whose native
// ABI is register-bounded. Functions retain a one-word path through 64 collector
// roots and use compact flat word arenas up to shared.GCFrameRootLimit. Each
// function gets independent compile state so railshot workers may populate maps
// in parallel.
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
	if !gcFrameTablesSafe(m) {
		return reject("table or element ownership is outside the exact native-root model")
	}
	funcImport := uint32(0)
	for i := range m.Imports {
		switch m.Imports[i].Type.Kind {
		case wasm.ExternFunc:
			ft, ok := m.FuncSignature(funcImport)
			funcImport++
			if !ok || !gcFrameCallABI(m, ft) {
				return reject("function import %d exceeds the exact native call ABI", funcImport-1)
			}
		case wasm.ExternGlobal:
			global := m.Imports[i].Type.GlobalType()
			if !collectorFrameRefType(m, global.Type) && !frameFunctionRefType(m, global.Type) {
				return reject("global import %d has an unsupported reference ownership shape", i)
			}
		case wasm.ExternTable:
			tableType := wasm.RefVal(m.Imports[i].Type.TableType().Ref)
			if !collectorFrameRefType(m, tableType) && !frameFunctionRefType(m, tableType) {
				return reject("table import %d has an unsupported reference ownership shape", i)
			}
		case wasm.ExternMem:
			// Linear-memory imports add no collector roots. Linking admission
			// separately proves exact same-domain ownership.
		case wasm.ExternTag:
			// Tag directories are immutable identities; active exception payloads
			// are covered by the function EH root maps below.
		default:
			return reject("import %d has unsupported external kind %d", i, m.Imports[i].Type.Kind)
		}
	}
	importedFunctions := m.ImportedFuncCount()
	ehMaps, err := railamd64.BuildExceptionRootMaps(m)
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
	modulePlan := &shared.GCModuleFrameRootPlan{Functions: make([]*shared.GCFrameRootPlan, len(m.Code))}
	var safepointBase uint32
functions:
	for function := range m.Code {
		if bodyHasUnsupportedNativeFrames(m, m.Code[function].BodyBytes, importedFunctions, len(m.Code)) {
			return reject("function %d contains an unsupported native call or frame shape", function)
		}
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return reject("function %d has no validated signature", function)
		}
		plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase, FixedOffsets: fixedRoots[function]}
		mayCollect := gcFrameBodyMayCollect(m.Code[function].BodyBytes)
		slot, local := 0, uint32(0)
		add := func(t wasm.ValType) bool {
			if collectorFrameRefType(m, t) {
				if len(plan.LocalOffsets) == shared.GCFrameRootLimit || slot > (math.MaxUint32-shared.AMD64FrameHeaderBytes)/8 {
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
		var liveMasks, callMasks []uint64
		var maskExtra gcFrameLivenessExtra
		var err error
		if bodyUsesEH(m.Code[function].BodyBytes) {
			liveMasks, callMasks, err = gcFrameConservativeMasks(m.Code[function].BodyBytes, len(plan.LocalIndexes), &maskExtra)
		} else {
			liveMasks, err = gcFrameLocalLiveness(m.Code[function].BodyBytes, plan.LocalIndexes, &callMasks, &maskExtra)
		}
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if bodyUsesNativeCall(m.Code[function].BodyBytes) && !gcFrameCallABI(m, ft) {
			return reject("function %d exceeds the exact native caller ABI", function)
		}
		if uint64(safepointBase)+uint64(len(liveMasks)) > uint64(shared.GCSafepointIDMax) {
			return reject("function %d exceeds the dense safepoint ID bound", function)
		}
		plan.LiveLocalMasks = liveMasks
		plan.LiveCallLocalMasks = callMasks
		plan.LiveMaskExtraWords = maskExtra.words
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
			if kind == 2 && e.Kind.Ref.Heap().Kind() == wasm.HeapAbs && e.Kind.Ref.Heap().Abs() == wasm.HeapI31 {
				// Validation and compileElemValues already proved each expression is
				// an exact immediate i31 or immutable global value; neither adds an
				// independent collector root beyond the global root.
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
				if !ok || gt.Mutable || !collectorFrameRefType(m, gt.Type) {
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

func gcFrameConservativeMasks(body []byte, localRoots int, extra *gcFrameLivenessExtra) (allocations, calls []uint64, err error) {
	return gcFrameAllLiveMasks(body, localRoots, extra)
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

func collectorFrameRefType(m *wasm.Module, t wasm.ValType) bool {
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
