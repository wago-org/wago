package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
)

func gcFrameFixedOffsets(rootMap *nativeabi.FunctionRootMap) []uint32 {
	gcRoots := 0
	for _, slot := range rootMap.Slots {
		if slot.Kind == nativeabi.RootGCRef {
			gcRoots++
		}
	}
	if gcRoots == 0 {
		return nil
	}
	offsets := make([]uint32, 0, gcRoots)
	for _, slot := range rootMap.Slots {
		if slot.Kind == nativeabi.RootGCRef {
			offsets = append(offsets, slot.Offset)
		}
	}
	return offsets
}

func gcFramePrepareModuleRootPlan(m *wasm.Module, classifier *wasm.ModuleInstructionClassifier, analysis *wasm.ValidatedModuleAnalysis) (*shared.GCModuleFrameRootPlan, error) {
	module := shared.NewGCModuleFrameRootPlan(len(m.Code))
	collectingFunctions := 0
	for function := range m.Code {
		mayCollect := false
		if analysis.ValidFor(m) {
			mayCollect = analysis.Funcs[function].Flags&wasm.ValidatedFuncMayCollect != 0
		} else {
			mayCollect = gcFrameBodyMayCollectWithClassifier(m.Code[function].BodyBytes, classifier)
		}
		if mayCollect {
			if !module.MarkFunction(function) {
				return nil, fmt.Errorf("function %d root plan ownership is invalid", function)
			}
			collectingFunctions++
		}
	}
	if !module.ReserveFunctions(collectingFunctions) {
		return nil, fmt.Errorf("root plan capacity %d is invalid", collectingFunctions)
	}
	return module, nil
}

// collectorFrameRefType classifies reference types represented by the Wasm GC
// collector. It deliberately excludes funcref, externref, and exnref. Indexed
// heap types are resolved in the containing module, including recursive groups;
// unresolved shapes fail closed by requesting GC handling.
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
		if m == nil {
			return true
		}
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

func collectorObjectFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind() != wasm.ValRef {
		return false
	}
	heap := t.Ref().Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapStruct, wasm.HeapArray:
			return true
		default:
			return false
		}
	case wasm.HeapDefType:
		kind, valid := heap.DefCompKind()
		return !valid || kind == wasm.CompStruct || kind == wasm.CompArray
	case wasm.HeapTypeIndex:
		if m == nil {
			return true
		}
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

func wasmFuncTypeTransfersCollectorRefs(m *wasm.Module, ft *wasm.CompType) bool {
	if ft == nil {
		return false
	}
	for _, typ := range ft.Params {
		if collectorObjectFrameRefType(m, typ) {
			return true
		}
	}
	for _, typ := range ft.Results {
		if collectorObjectFrameRefType(m, typ) {
			return true
		}
	}
	return false
}

func wasmFuncTypeReferenceFree(ft *wasm.CompType) bool {
	if ft == nil {
		return false
	}
	for _, typ := range ft.Params {
		if typ.Kind() == wasm.ValRef {
			return false
		}
	}
	for _, typ := range ft.Results {
		if typ.Kind() == wasm.ValRef {
			return false
		}
	}
	return true
}

func moduleHasCollectorReferenceCallBoundary(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != wasm.ExternFunc {
			continue
		}
		ft, ok := m.ImportFuncType(i)
		if !ok || wasmFuncTypeTransfersCollectorRefs(m, ft) {
			return true
		}
	}
	return false
}

func moduleHasGCAllocationSites(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		if gcFrameBodyMayAllocateWithClassifier(m.Code[i].BodyBytes, &classifier) {
			return true
		}
	}
	return false
}

func moduleHasGCAllocationSitesWithValidation(m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis) bool {
	if analysis.ValidFor(m) {
		return analysis.Flags&wasm.ValidatedFuncMayAllocate != 0
	}
	return moduleHasGCAllocationSites(m)
}

// GCNativeRootAdmission describes whether a compiled generic-GC module can
// collect while native frames are active. Reason is populated for fail-closed
// collection-disabled admission. MetadataBytes is the direct serialized root-map
// payload estimate, excluding outer codec section framing.
type GCNativeRootAdmission struct {
	Required      bool
	Exact         bool
	Reason        string
	Safepoints    uint32
	Callsites     uint32
	MaximumRoots  uint32
	MetadataBytes uint64
}

// GCNativeRootAdmission reports exact native-root coverage and actionable
// fail-closed diagnostics without exposing live frames or process-local handles.
func (c *Compiled) GCNativeRootAdmission() GCNativeRootAdmission {
	status := GCNativeRootAdmission{Required: c != nil && c.needsExactNativeGCRoots()}
	if c == nil {
		status.Reason = "nil compiled module"
		return status
	}
	rootMap := c.genericGCFrameRoots()
	if rootMap == nil {
		if !status.Required {
			status.Reason = "module does not require exact native GC roots"
		} else {
			status.Reason = c.gcRootAdmissionFailure()
		}
		return status
	}
	status.Exact = true
	status.Safepoints = uint32(len(rootMap.safepoints))
	status.Callsites = uint32(len(rootMap.callsites))
	status.MetadataBytes = uint64(compiledUvarintLen(uint64(len(rootMap.adapterReturnOffsets)))+len(rootMap.adapterReturnOffsets)*4) +
		uint64(compiledUvarintLen(uint64(len(rootMap.safepoints)))) +
		uint64(compiledUvarintLen(uint64(len(rootMap.callsites))))
	for i := range rootMap.safepoints {
		roots := len(rootMap.safepoints[i].offsets)
		if roots > int(status.MaximumRoots) {
			status.MaximumRoots = uint32(roots)
		}
		status.MetadataBytes += uint64(8 + compiledUvarintLen(uint64(roots)) + roots*4)
	}
	for i := range rootMap.callsites {
		roots := len(rootMap.callsites[i].offsets)
		if roots > int(status.MaximumRoots) {
			status.MaximumRoots = uint32(roots)
		}
		status.MetadataBytes += uint64(12 + compiledUvarintLen(uint64(roots)) + roots*4)
	}
	return status
}

func gcFrameCollectorElementExprSafe(expr wasm.Expr) bool {
	body := expr.BodyBytes
	if len(body) == 0 {
		encoded, err := wasm.EncodeExpr(expr)
		if err != nil {
			return false
		}
		body = encoded
	}
	// Module validation and elementPayloads perform the full type/opcode proof.
	// Frame-root admission only needs to recognize the retained, single-result GC
	// expression form so its passive slot can participate in persistent roots.
	return len(body) != 0 && body[len(body)-1] == 0x0b
}

func validGCModuleFrameRootPlan(module *shared.GCModuleFrameRootPlan) bool {
	if module == nil || module.FunctionCount() == 0 {
		return false
	}
	totalSafepoints, totalCallsites := 0, 0
	var previousID uint32
	for function := 0; function < module.FunctionCount(); function++ {
		if module.FunctionPending(function) {
			return false // fail closed if a producer omitted a collecting function
		}
		plan := module.Function(function)
		if plan == nil {
			continue // proven non-collecting function; no active-frame map is needed
		}
		if !plan.Candidate || !plan.Exact || !plan.ValidLiveMasks() || plan.AllocationMaskCount() != plan.SafepointCount() || len(plan.Locals) > shared.GCFrameTrackedLocalLimit {
			return false
		}
		active := plan.SafepointCount() != 0 || plan.CallsiteCount() != 0
		if active && plan.FrameBytes < 8 {
			return false
		}
		if active && !validGCFrameLocals(plan.Locals, plan.FrameBytes) {
			return false
		}
		var previousReturn uint32
		if !plan.VisitCallsites(func(i int, callsite shared.GCFrameCallsite) bool {
			returnOffset, stackAdjust := callsite.ReturnOffset(), callsite.StackAdjust()
			if returnOffset == 0 || (i != 0 && returnOffset <= previousReturn) || stackAdjust%8 != 0 || stackAdjust > 1<<20 || !validGCFrameOffsets(callsite.Offsets(), plan.FrameBytes) {
				return false
			}
			previousReturn = returnOffset
			totalCallsites++
			return true
		}) {
			return false
		}
		if !plan.VisitSafepoints(func(i int, offsets []uint32) bool {
			id64 := uint64(plan.SafepointBase) + uint64(i) + 1
			if id64 == 0 || id64 > uint64(shared.GCSafepointIDMax) || (totalSafepoints != 0 && uint32(id64) <= previousID) || !validGCFrameOffsets(offsets, plan.FrameBytes) {
				return false
			}
			previousID = uint32(id64)
			totalSafepoints++
			return true
		}) {
			return false
		}
	}
	return totalSafepoints != 0 || totalCallsites != 0
}

func validateCompiledGCFrameRoots(c *Compiled, rootMap *compiledGCFrameRoots) error {
	if rootMap == nil {
		return nil
	}
	if c == nil || !c.needsExactNativeGCRoots() {
		return fmt.Errorf("GC frame-root metadata requires exact native GC roots")
	}
	if !validCompiledGCFunctionTables(c) || len(c.Funcs) == 0 {
		return fmt.Errorf("GC frame-root metadata requires a validated local call graph with private tables")
	}
	if c.HasStart && (c.StartIsImport || c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Funcs)) {
		return fmt.Errorf("GC frame-root metadata requires an exact local start function")
	}
	for i := range c.GlobalImports {
		global := c.GlobalImports[i]
		switch global.Type {
		case ValI32, ValI64, ValF32, ValF64, ValV128, ValFuncRef, ValExternRef, ValExnRef, ValAnyRef, ValI31Ref:
		default:
			return fmt.Errorf("GC frame-root metadata rejects global import %d", i)
		}
	}
	if c.NumImports != len(c.Imports) || len(c.importFuncSigs) != len(c.Imports) {
		return fmt.Errorf("GC frame-root metadata has inconsistent function imports")
	}
	for i := range c.importFuncSigs {
		if !gcFramePublicCallABI(c.importFuncSigs[i]) {
			return fmt.Errorf("GC frame-root metadata rejects import %d signature", i)
		}
	}
	if len(rootMap.safepoints) == 0 && len(rootMap.callsites) == 0 {
		return fmt.Errorf("GC frame-root metadata has no safepoints or callsites")
	}
	if len(rootMap.safepoints) == 0 && c.usesGenericGCExecution() && !c.hasCollectorReferenceCallBoundary() {
		return fmt.Errorf("generic GC frame-root metadata has no allocation safepoints")
	}
	var previousAdapter uint32
	for i, off := range rootMap.adapterReturnOffsets {
		if off == 0 || uint64(off) >= uint64(len(c.code)) || (i != 0 && off <= previousAdapter) {
			return fmt.Errorf("GC frame-root adapter return offset %d is invalid", off)
		}
		previousAdapter = off
	}
	var previousReturn uint32
	for i := range rootMap.callsites {
		callsite := &rootMap.callsites[i]
		if callsite.frameBytes < 8 || callsite.frameBytes > 1<<31-1 || callsite.returnOffset == 0 || uint64(callsite.returnOffset) >= uint64(len(c.code)) || (i != 0 && callsite.returnOffset <= previousReturn) || callsite.stackAdjust%8 != 0 || callsite.stackAdjust > 1<<20 || !validGCFrameOffsets(callsite.offsets, callsite.frameBytes) {
			return fmt.Errorf("GC frame-root callsite %d is malformed", callsite.returnOffset)
		}
		previousReturn = callsite.returnOffset
	}
	var previousID uint32
	for i := range rootMap.safepoints {
		safepoint := &rootMap.safepoints[i]
		if safepoint.frameBytes < 8 || safepoint.frameBytes > 1<<31-1 || safepoint.id == 0 || safepoint.id > shared.GCSafepointIDMax || (i != 0 && safepoint.id <= previousID) || !validGCFrameOffsets(safepoint.offsets, safepoint.frameBytes) {
			return fmt.Errorf("GC frame-root safepoint %d is malformed", safepoint.id)
		}
		previousID = safepoint.id
	}
	return nil
}

func validCompiledGCFunctionTables(c *Compiled) bool {
	if c == nil {
		return false
	}
	totalFuncs := c.NumImports + len(c.Funcs)
	validValues := func(refType ValType, values []RefInit) bool {
		refType = normalizedElemRefType(refType)
		for _, value := range values {
			if len(value.Expr) != 0 {
				if !c.usesGenericGCExecution() || (refType != ValAnyRef && refType != ValI31Ref) || value.Expr[len(value.Expr)-1] != 0x0b {
					return false
				}
				continue
			}
			if value.HasGlobal {
				if int(value.GlobalIndex) >= len(c.Globals) || c.Globals[value.GlobalIndex].Mutable {
					return false
				}
				if value.I31Wrap {
					if refType != ValI31Ref || c.Globals[value.GlobalIndex].Type != ValI32 {
						return false
					}
				} else if !isGCRefValType(c.Globals[value.GlobalIndex].Type) {
					return false
				}
				continue
			}
			if value.I31Wrap {
				return false
			}
			if refType == ValFuncRef {
				if !value.Null && int(value.FuncIndex) >= totalFuncs {
					return false
				}
				continue
			}
			if refType == ValI31Ref {
				if !value.Null && value.FuncIndex&1 == 0 {
					return false
				}
				continue
			}
			if !isGCRefValType(refType) || !value.Null {
				return false
			}
		}
		return true
	}
	for tableIndex := 0; tableIndex < c.tableCount(); tableIndex++ {
		typ := c.tableElementType(tableIndex)
		if typ != ValFuncRef && !isGCRefValType(typ) {
			return false
		}
		if tableIndex == 0 {
			if c.HasTableInitFunc && (typ != ValFuncRef || int(c.TableInitFunc) >= totalFuncs) {
				return false
			}
			continue
		}
		table := &c.extraTables[tableIndex-1]
		if table.HasInitFunc && (typ != ValFuncRef || int(table.InitFunc) >= totalFuncs) {
			return false
		}
	}
	for i := range c.Elems {
		elem := &c.Elems[i]
		if elem.Mode != ElemModeActive || int(elem.TableIndex) >= c.tableCount() {
			return false
		}
		if normalizedElemRefType(elem.RefType) != c.tableElementType(int(elem.TableIndex)) || !validValues(elem.RefType, elem.Values) {
			return false
		}
	}
	for i := range c.passiveElems {
		elem := &c.passiveElems[i]
		if (elem.Mode != ElemModeDeclarative && elem.Mode != ElemModePassive) || !validValues(elem.RefType, elem.Values) {
			return false
		}
	}
	return true
}

func gcFramePublicCallABI(sig FuncSig) bool {
	slots := func(types []ValType) (int, bool) {
		n := 0
		for _, typ := range types {
			switch typ {
			case ValV128:
				n += 2
			case ValI32, ValI64, ValF32, ValF64, ValFuncRef, ValExternRef, ValAnyRef, ValExnRef, ValI31Ref:
				n++
			default:
				return 0, false
			}
		}
		return n, true
	}
	params, ok := slots(sig.Params)
	if !ok || params > 64 {
		return false
	}
	results, ok := slots(sig.Results)
	return ok && results <= 64
}

func validGCFrameOffsets(offsets []uint32, frameBytes uint32) bool {
	if len(offsets) != 0 && frameBytes < 8 {
		return false
	}
	var previous uint32
	for i, off := range offsets {
		if off%8 != 0 || off > frameBytes-8 || (i != 0 && off <= previous) {
			return false
		}
		previous = off
	}
	return true
}

func validGCFrameLocals(locals []shared.GCFrameLocal, frameBytes uint32) bool {
	if len(locals) != 0 && frameBytes < 8 {
		return false
	}
	var previousIndex, previousOffset uint32
	for i, local := range locals {
		off := local.Offset
		if off%8 != 0 || off > frameBytes-8 ||
			(i != 0 && (local.Index <= previousIndex || off <= previousOffset)) {
			return false
		}
		previousIndex, previousOffset = local.Index, off
	}
	return true
}
