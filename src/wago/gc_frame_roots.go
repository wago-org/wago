package wago

import (
	"fmt"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const gcNativeFrameRootLimit = shared.GCFrameRootLimit

func (in *Instance) gcCompilerCallsite(pc uintptr) (*compiledGCFrameRoots, uintptr, uintptr, int) {
	compiled, codeBase := in.compilerGenerationForPC(pc)
	if compiled == nil {
		return nil, 0, 0, -1
	}
	plan := compiled.genericGCFrameRoots()
	if plan == nil {
		return nil, 0, 0, -1
	}
	rel := uint32(pc - codeBase)
	for i := range plan.callsites {
		if plan.callsites[i].returnOffset == rel {
			return plan, codeBase, uintptr(len(compiled.code)), i
		}
	}
	return nil, 0, 0, -1
}

// collectorFrameRefType classifies reference types represented by the Wasm GC
// collector. It deliberately excludes funcref, externref, and exnref. Indexed
// heap types are resolved in the containing module, including recursive groups;
// unresolved shapes fail closed by requesting GC handling.
func collectorFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	return codegen.IsCollectorReferenceType(m, t)
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
	for i := 0; i < m.ImportedFuncCount(); i++ {
		ft, ok := m.FuncSignature(uint32(i))
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

func moduleHasCollectorReferenceFrames(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for local := range m.Code {
		if !gcFrameBodyMayCollectWithClassifier(m.Code[local].BodyBytes, &classifier) {
			continue
		}
		ft, ok := m.LocalFuncType(local)
		if !ok || ft == nil {
			return true
		}
		for _, typ := range ft.Params {
			if collectorFrameRefType(m, typ) {
				return true
			}
		}
		for _, typ := range ft.Results {
			if collectorFrameRefType(m, typ) {
				return true
			}
		}
		for _, run := range m.Code[local].Locals.Runs {
			if collectorFrameRefType(m, run.Type) {
				return true
			}
		}
	}
	return false
}

func draglineGCFrameRoots(output corecompiler.Output) (*compiledGCFrameRoots, error) {
	if output.Engine != corecompiler.EngineDragline {
		return nil, fmt.Errorf("GC frame roots require Dragline output")
	}
	if len(output.GCCallsites) == 0 && len(output.GCSafepoints) == 0 {
		return nil, fmt.Errorf("Dragline produced no GC root metadata")
	}
	rootMap := &compiledGCFrameRoots{
		callsites:  make([]compiledGCFrameCallsite, 0, len(output.GCCallsites)),
		safepoints: make([]compiledGCFrameSafepoint, 0, len(output.GCSafepoints)),
	}
	for index, offset := range output.GCAdapterReturnOffsets {
		if offset == 0 || uint64(offset) >= uint64(len(output.Code)) || index != 0 && offset <= output.GCAdapterReturnOffsets[index-1] {
			return nil, fmt.Errorf("Dragline GC adapter return offset %d is malformed", index)
		}
	}
	rootMap.adapterReturnOffsets = append(rootMap.adapterReturnOffsets, output.GCAdapterReturnOffsets...)
	var interner gcFrameOffsetInterner
	safepointRootEnd := uint64(0)
	previousID := uint32(0)
	for index, safepoint := range output.GCSafepoints {
		nextRootEnd := uint64(safepoint.RootStart) + uint64(safepoint.RootCount)
		if uint64(safepoint.RootStart) != safepointRootEnd || nextRootEnd > uint64(len(output.GCSafepointRoots)) || safepoint.ID == 0 || safepoint.ID > shared.GCSafepointIDMax || index != 0 && safepoint.ID <= previousID || safepoint.FrameBytes < 8 || safepoint.FrameBytes > 1<<31-1 {
			return nil, fmt.Errorf("Dragline GC safepoint %d is malformed", index)
		}
		offsets := output.GCSafepointRoots[safepoint.RootStart:nextRootEnd]
		if len(offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(offsets, safepoint.FrameBytes) {
			return nil, fmt.Errorf("Dragline GC safepoint %d roots are malformed", index)
		}
		rootMap.safepoints = append(rootMap.safepoints, compiledGCFrameSafepoint{id: safepoint.ID, frameBytes: safepoint.FrameBytes, offsets: interner.intern(offsets, true)})
		safepointRootEnd = nextRootEnd
		previousID = safepoint.ID
	}
	if safepointRootEnd != uint64(len(output.GCSafepointRoots)) {
		return nil, fmt.Errorf("Dragline GC safepoint root slab is not canonically packed")
	}
	rootEnd := uint64(0)
	previousReturn := uint32(0)
	for index, callsite := range output.GCCallsites {
		nextRootEnd := uint64(callsite.RootStart) + uint64(callsite.RootCount)
		if uint64(callsite.RootStart) != rootEnd || nextRootEnd > uint64(len(output.GCRoots)) || callsite.ReturnOffset == 0 || uint64(callsite.ReturnOffset) >= uint64(len(output.Code)) || index != 0 && callsite.ReturnOffset <= previousReturn || callsite.FrameBytes < 8 || callsite.FrameBytes > 1<<31-1 || callsite.StackAdjust&7 != 0 || callsite.StackAdjust > 1<<20 {
			return nil, fmt.Errorf("Dragline GC callsite %d is malformed", index)
		}
		offsets := output.GCRoots[callsite.RootStart:nextRootEnd]
		if len(offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(offsets, callsite.FrameBytes) {
			return nil, fmt.Errorf("Dragline GC callsite %d roots are malformed", index)
		}
		rootMap.callsites = append(rootMap.callsites, compiledGCFrameCallsite{
			returnOffset: callsite.ReturnOffset,
			frameBytes:   callsite.FrameBytes,
			stackAdjust:  callsite.StackAdjust,
			offsets:      interner.intern(offsets, true),
		})
		rootEnd = nextRootEnd
		previousReturn = callsite.ReturnOffset
	}
	if rootEnd != uint64(len(output.GCRoots)) {
		return nil, fmt.Errorf("Dragline GC root slab is not canonically packed")
	}
	return rootMap, nil
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
	if module == nil || len(module.Functions) == 0 {
		return false
	}
	totalSafepoints, totalCallsites := 0, 0
	var previousID uint32
	for _, plan := range module.Functions {
		if plan == nil {
			continue // proven non-collecting function; no active-frame map is needed
		}
		if !plan.Candidate || !plan.Exact || !plan.ValidLiveMasks() || len(plan.LiveLocalMasks) != len(plan.Safepoints) || len(plan.LocalIndexes) != len(plan.LocalOffsets) || len(plan.LocalOffsets) > shared.GCFrameTrackedLocalLimit {
			return false
		}
		active := len(plan.Safepoints) != 0 || len(plan.Callsites) != 0
		if active && plan.FrameBytes < 8 {
			return false
		}
		if active && !validGCFrameOffsets(plan.LocalOffsets, plan.FrameBytes) {
			return false
		}
		var previousReturn uint32
		for i := range plan.Callsites {
			callsite := &plan.Callsites[i]
			if callsite.ReturnOffset == 0 || (i != 0 && callsite.ReturnOffset <= previousReturn) || callsite.StackAdjust%8 != 0 || callsite.StackAdjust > 1<<20 || len(callsite.Offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(callsite.Offsets, plan.FrameBytes) {
				return false
			}
			previousReturn = callsite.ReturnOffset
			totalCallsites++
		}
		for i := range plan.Safepoints {
			safepoint := &plan.Safepoints[i]
			if safepoint.ID == 0 || safepoint.ID > shared.GCSafepointIDMax || (totalSafepoints != 0 && safepoint.ID <= previousID) || len(safepoint.Offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(safepoint.Offsets, plan.FrameBytes) {
				return false
			}
			previousID = safepoint.ID
			totalSafepoints++
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
		if callsite.frameBytes < 8 || callsite.frameBytes > 1<<31-1 || callsite.returnOffset == 0 || uint64(callsite.returnOffset) >= uint64(len(c.code)) || (i != 0 && callsite.returnOffset <= previousReturn) || callsite.stackAdjust%8 != 0 || callsite.stackAdjust > 1<<20 || len(callsite.offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(callsite.offsets, callsite.frameBytes) {
			return fmt.Errorf("GC frame-root callsite %d is malformed", callsite.returnOffset)
		}
		previousReturn = callsite.returnOffset
	}
	var previousID uint32
	for i := range rootMap.safepoints {
		safepoint := &rootMap.safepoints[i]
		if safepoint.frameBytes < 8 || safepoint.frameBytes > 1<<31-1 || safepoint.id == 0 || safepoint.id > shared.GCSafepointIDMax || (i != 0 && safepoint.id <= previousID) || len(safepoint.offsets) > gcNativeFrameRootLimit || !validGCFrameOffsets(safepoint.offsets, safepoint.frameBytes) {
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
