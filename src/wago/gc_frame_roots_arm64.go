//go:build (linux || darwin) && arm64 && !wago_precompiled

package wago

import (
	"fmt"

	railarm64 "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// newGCFrameRootPlan admits the bounded exact arm64 collection product. Direct
// local and synchronous host calls may use the wrapper ABI. Dynamic indirect,
// reference, and tail calls retain the smaller register-ABI proof. The plan
// supports liveness-exact collector locals, hidden operand spills, direct and
// recursive calls, direct host re-entry, same-domain foreign calls,
// mutable/shared GC globals and collector tables, and fixed EH payload roots.
func newGCFrameRootPlan(m *wasm.Module, exactRoots bool, diagnostic *string, analysis *wasm.ValidatedModuleAnalysis) *shared.GCModuleFrameRootPlan {
	if diagnostic != nil {
		*diagnostic = ""
	}
	if !exactRoots {
		return nil
	}
	reject := func(format string, args ...any) *shared.GCModuleFrameRootPlan {
		if diagnostic != nil {
			*diagnostic = fmt.Sprintf(format, args...)
		}
		return nil
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
	collectorBoundary := moduleHasCollectorReferenceCallBoundary(m)
	funcImport := uint32(0)
	for i := range m.Imports {
		switch m.Imports[i].Type.Kind {
		case wasm.ExternFunc:
			ft, ok := m.FuncSignature(funcImport)
			funcImport++
			if !ok {
				return reject("function import %d has no validated signature", funcImport-1)
			}
			// Reference-free imports use the parked synchronous wrapper and need
			// not also fit the internal register ABI. Preserve the existing
			// collector-boundary path so reference ownership and root handling
			// remain coupled to that module-level proof.
			if collectorBoundary || wasmFuncTypeReferenceFree(ft) {
				if !arm64GCFrameHostCallABI(ft) {
					return reject("function import %d exceeds the synchronous host-call ABI", funcImport-1)
				}
			} else if !arm64GCFrameReferenceCallABI(m, ft) {
				return reject("function import %d exceeds the exact native call ABI", funcImport-1)
			}
		case wasm.ExternGlobal:
			global := m.Imports[i].Type.GlobalType()
			if !collectorBoundary && !collectorFrameRefType(m, global.Type) && !arm64FunctionFrameRefType(m, global.Type) {
				return reject("global import %d has an unsupported reference ownership shape", i)
			}
		case wasm.ExternTable:
			tableType := wasm.RefVal(m.Imports[i].Type.TableType().Ref)
			if !collectorFrameRefType(m, tableType) && !arm64FunctionFrameRefType(m, tableType) {
				return reject("table import %d has an unsupported reference ownership shape", i)
			}
		case wasm.ExternMem:
		case wasm.ExternTag:
		default:
			return reject("import %d has unsupported external kind %d", i, m.Imports[i].Type.Kind)
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	ehMaps, err := railarm64.BuildExceptionRootMaps(m)
	if err != nil {
		return reject("exception root maps: %v", err)
	}
	module, err := gcFramePrepareModuleRootPlan(m, &classifier, analysis)
	if err != nil {
		return reject("%v", err)
	}
	var safepointBase uint32
	ehMapIndex := 0
	for function := range m.Code {
		hasFixedRoots := false
		if ehMapIndex < len(ehMaps) && ehMaps[ehMapIndex].LocalFunction == uint32(function) {
			hasFixedRoots = true
			ehMapIndex++
		}
		if !module.FunctionPending(function) {
			continue // RootNone: no safepoint can observe this frame.
		}
		var fixedOffsets []uint32
		if hasFixedRoots {
			fixedOffsets = gcFrameFixedOffsets(&ehMaps[ehMapIndex-1])
		}
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return reject("function %d has no validated signature", function)
		}
		plan, ok := module.BeginFunction(function)
		if !ok {
			return reject("function %d root plan ownership is invalid", function)
		}
		*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, SafepointBase: safepointBase}
		if !plan.SetFixedOffsets(fixedOffsets) {
			return reject("function %d has invalid fixed root offsets", function)
		}
		slot, local := 0, uint32(0)
		add := func(t wasm.ValType) bool {
			if collectorFrameRefType(m, t) {
				if len(plan.Locals) == shared.GCFrameTrackedLocalLimit {
					return false
				}
				plan.Locals = append(plan.Locals, shared.GCFrameLocal{Index: local, Offset: uint32(shared.ARM64FrameHeaderBytes + slot*8)})
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
				return reject("function %d exceeds %d tracked collector locals", function, shared.GCFrameTrackedLocalLimit)
			}
		}
		for _, run := range m.Code[function].Locals.Runs {
			for i := uint32(0); i < run.Count; i++ {
				if !add(run.Type) {
					return reject("function %d exceeds %d tracked collector locals", function, shared.GCFrameTrackedLocalLimit)
				}
			}
		}
		if !arm64GCFrameBodySafe(m, m.Code[function].BodyBytes, &classifier, collectorBoundary) {
			return reject("function %d contains an unsupported native call or frame shape", function)
		}
		var liveMasks gcFrameLiveMasks
		plan.Conservative = arm64BodyUsesEH(m.Code[function].BodyBytes, &classifier) || gcFramePreferConservativeMasks(len(plan.Locals), len(m.Code[function].BodyBytes))
		if plan.Conservative {
			liveMasks, err = arm64GCFrameConservativeMasks(m.Code[function].BodyBytes, len(plan.Locals), &classifier)
		} else {
			liveMasks, err = gcFrameLocalLivenessArenaWithClassifier(m.Code[function].BodyBytes, plan.Locals, &classifier)
		}
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		plan.Locals, liveMasks, _, err = gcFrameCompactLiveLocalsArena(plan.Locals, liveMasks)
		if err != nil {
			return reject("function %d exact local liveness: %v", function, err)
		}
		if uint64(safepointBase)+uint64(liveMasks.allocationN) > uint64(shared.GCSafepointIDMax) {
			return reject("function %d exceeds the dense safepoint ID bound", function)
		}
		if !plan.SetLiveMasks(liveMasks.words, liveMasks.allocationN, liveMasks.callN) {
			return reject("function %d has malformed exact local liveness masks", function)
		}
		safepointBase += uint32(liveMasks.allocationN)
	}
	if ehMapIndex != len(ehMaps) {
		return reject("exception root map function %d is out of range", ehMaps[ehMapIndex].LocalFunction)
	}
	return module
}

func arm64GCFrameTablesSafe(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	totalFuncs := m.ImportedFuncCount() + len(m.Code)
	functionIndexOK := func(index uint32) bool { return int(index) < totalFuncs }
	tableKinds := make([]uint8, m.TableCount())
	for tableIndex := 0; tableIndex < m.TableCount(); tableIndex++ {
		tableType, ok := m.TableType(uint32(tableIndex))
		if !ok {
			return false
		}
		typ := wasm.RefVal(tableType.Ref)
		switch {
		case arm64FunctionFrameRefType(m, typ):
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
		} else if e.Kind.Kind == wasm.ElemFuncs || arm64FunctionFrameRefType(m, wasm.RefVal(e.Kind.Ref)) {
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

func arm64GCFrameBodySafe(m *wasm.Module, body []byte, classifier *wasm.ModuleInstructionClassifier, collectorBoundary bool) bool {
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
		case 0x10:
			if int(imm.Index) >= m.ImportedFuncCount()+len(m.Code) {
				return false
			}
			ft, ok := m.FuncSignature(imm.Index)
			if !ok {
				return false
			}
			if int(imm.Index) < m.ImportedFuncCount() {
				if collectorBoundary || wasmFuncTypeReferenceFree(ft) {
					if !arm64GCFrameHostCallABI(ft) {
						return false
					}
				} else if !arm64GCFrameReferenceCallABI(m, ft) {
					return false
				}
			}
		case 0x11:
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || !arm64GCFrameReferenceCallABI(m, ft) {
				return false
			}
		case 0x12:
			if int(imm.Index) >= m.ImportedFuncCount()+len(m.Code) {
				return false
			}
			ft, ok := m.FuncSignature(imm.Index)
			if !ok || !arm64GCFrameReferenceCallABI(m, ft) || funcTypeSlotsForRoots(ft.Params) > abi.TailArgsSlots {
				return false
			}
		case 0x13:
			if imm.Index2 != 0 || !arm64GCFunctionTableMonomorphic(m) {
				return false
			}
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || funcTypeSlotsForRoots(ft.Params) > abi.TailArgsSlots {
				return false
			}
		case 0x14:
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || !arm64GCFrameReferenceCallABI(m, ft) {
				return false
			}
		case 0x15:
			ft, ok := m.TypeFunc(imm.Index)
			if !ok || funcTypeSlotsForRoots(ft.Params) > abi.TailArgsSlots {
				return false
			}
		case 0xd2:
			if int(imm.Index) >= m.ImportedFuncCount()+len(m.Code) {
				return false
			}
		case 0x06, 0x07, 0x09, 0x18, 0x19:
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

func arm64GCFrameConservativeMasks(body []byte, localRoots int, classifier *wasm.ModuleInstructionClassifier) (gcFrameLiveMasks, error) {
	return gcFrameAllLiveMasksArenaWithClassifier(body, localRoots, classifier)
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

func arm64GCFrameHostCallABI(ft *wasm.CompType) bool {
	if ft == nil {
		return false
	}
	slots := func(types []wasm.ValType) (int, bool) {
		n := 0
		for _, typ := range types {
			switch {
			case wasm.EqualValType(typ, wasm.V128):
				n += 2
			case wasm.EqualValType(typ, wasm.I32), wasm.EqualValType(typ, wasm.I64), wasm.EqualValType(typ, wasm.F32), wasm.EqualValType(typ, wasm.F64), typ.Kind() == wasm.ValRef:
				n++
			default:
				return 0, false
			}
		}
		return n, true
	}
	params, ok := slots(ft.Params)
	if !ok || params > 64 {
		return false
	}
	results, ok := slots(ft.Results)
	return ok && results <= 64
}

func arm64GCFrameReferenceCallABI(m *wasm.Module, ft *wasm.CompType) bool {
	if ft == nil || len(ft.Results) > 2 {
		return false
	}
	gp, fp := 0, 0
	for _, t := range ft.Params {
		switch {
		case wasm.EqualValType(t, wasm.I32), wasm.EqualValType(t, wasm.I64), collectorFrameRefType(m, t), arm64FunctionFrameRefType(m, t):
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
		return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64) || collectorFrameRefType(m, t) || arm64FunctionFrameRefType(m, t)
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
