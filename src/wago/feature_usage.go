package wago

import (
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type moduleRequirements struct {
	features             CoreFeatures
	elemStateCount       int
	dataStateCount       int
	moduleFacts          *frontend.ModuleFacts
	atomicWaitHelpers    bool
	indexedFuncRefTest   bool
	indexedFuncRefCast   bool
	arm64GCRefTestHelper bool
}

// moduleRequiredFeatures records optional core features that remain execution
// dependencies of the compiled artifact. Codec version 2 stores the full public
// CoreFeatures mask and rejects unknown bits. Compile-time-only features such as
// extended constant expressions are folded into initializer metadata.
func moduleRequiredFeatures(m *wasm.Module) CoreFeatures {
	return analyzeModuleRequirements(m).features
}

func analyzeModuleRequirements(m *wasm.Module) moduleRequirements {
	return analyzeModuleRequirementsWithValidation(m, nil)
}

func analyzeModuleRequirementsWithValidation(m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis) moduleRequirements {
	if m == nil {
		return moduleRequirements{}
	}
	var out CoreFeatures
	programmaticCode := false
	elemStateCount, dataStateCount := 0, 0
	moduleFacts := frontend.NewModuleFacts(m.TableCount(), m.MemCount())
	atomicWaitHelpers := false
	indexedFuncRefTest, indexedFuncRefCast := false, false
	arm64GCRefTestHelper := false
	if frontend.ModuleNonCodeRequiresSIMD(m) {
		out |= CoreFeatureSIMD
	}
	if m.TagCount() != 0 {
		out |= CoreFeatureExceptionHandling
	}

	for _, rec := range m.Types {
		if len(rec.SubTypes) != 1 {
			// Empty and multi-member rec groups use the standardized recursive
			// type-section grammar even when all members are plain functions.
			out |= CoreFeatureTypedFunctionReferences
		}
		for _, sub := range rec.SubTypes {
			if sub.HasPrefix || len(sub.Supers) != 0 {
				out |= CoreFeatureGC
			}
			out |= requiredFeaturesForCompositeType(sub.Comp)
		}
	}
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternGlobal:
			out |= requiredFeaturesForValType(im.Type.GlobalType().Type)
			if im.Type.GlobalType().Mutable {
				out |= CoreFeatureMutableGlobal
			}
		case wasm.ExternTable:
			table := im.Type.TableType()
			out |= requiredFeaturesForTableRef(table.Ref)
			if table.Limits.Addr64 {
				out |= CoreFeatureTable64
			}
		}
	}
	for _, g := range m.Globals {
		out |= requiredFeaturesForValType(g.Type.Type)
		features, usesRefFunc := analyzeConstExprRequirements(g.Init, m.ImportedGlobalCount())
		out |= features
		moduleFacts.UsesRefFunc = moduleFacts.UsesRefFunc || usesRefFunc
	}
	for _, ex := range m.Exports {
		switch ex.Index.Kind {
		case wasm.ExternGlobal:
			if gt, ok := m.GlobalTypeByIndex(uint32(ex.Index.Index)); ok && gt.Mutable {
				out |= CoreFeatureMutableGlobal
			}
		case wasm.ExternTable:
			if uint64(ex.Index.Index) < uint64(len(moduleFacts.TableExported)) {
				moduleFacts.TableExported[ex.Index.Index] = true
			}
		case wasm.ExternMem:
			if uint64(ex.Index.Index) < uint64(len(moduleFacts.MemoryExported)) {
				moduleFacts.MemoryExported[ex.Index.Index] = true
			}
		}
	}
	if m.TableCount() > 1 {
		out |= CoreFeatureReferenceTypes
	}
	if m.ImportedMemCount()+len(m.Memories) > 1 {
		out |= CoreFeatureMultiMemory
	}
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternMem {
			if im.Type.MemType().Limits.Addr64 {
				out |= CoreFeatureMemory64
			}
			if im.Type.MemType().Shared {
				out |= CoreFeatureThreads
			}
		}
	}
	for _, memory := range m.Memories {
		if memory.Limits.Addr64 {
			out |= CoreFeatureMemory64
		}
		if memory.Shared {
			out |= CoreFeatureThreads
		}
	}
	for _, table := range m.Tables {
		out |= requiredFeaturesForTableRef(table.Type.Ref)
		if table.Init != nil {
			out |= CoreFeatureReferenceTypes
		}
		if table.Init != nil {
			out |= requiredFeaturesForConstExpr(*table.Init, m.ImportedGlobalCount())
		}
		if table.Type.Limits.Addr64 {
			out |= CoreFeatureTable64
		}
	}
	for i, elem := range m.Elements {
		if elem.Mode.Kind == wasm.ElemActive {
			out |= requiredFeaturesForConstExpr(elem.Mode.Offset, m.ImportedGlobalCount())
		}
		if elem.Kind.Kind == wasm.ElemTypedExprs {
			out |= requiredFeaturesForValType(wasm.RefVal(elem.Kind.Ref))
		}
		for _, expr := range elem.Kind.Exprs {
			features, usesRefFunc := analyzeConstExprRequirements(expr, m.ImportedGlobalCount())
			out |= features
			moduleFacts.UsesRefFunc = moduleFacts.UsesRefFunc || usesRefFunc
		}
		moduleFacts.UsesRefFunc = moduleFacts.UsesRefFunc || (elem.Kind.Kind == wasm.ElemFuncs && len(elem.Kind.Funcs) != 0)
		if elem.Mode.Kind != wasm.ElemActive {
			out |= CoreFeatureBulkMemoryOperations
		}
		if elem.Mode.Kind == wasm.ElemPassive {
			elemStateCount = i + 1
		}
		if elem.Kind.Kind != wasm.ElemFuncs {
			out |= CoreFeatureReferenceTypes
		}
	}
	for i, data := range m.Data {
		if data.Mode.Kind == wasm.DataActive {
			out |= requiredFeaturesForConstExpr(data.Mode.Offset, m.ImportedGlobalCount())
		}
		if data.Mode.Kind == wasm.DataPassive {
			out |= CoreFeatureBulkMemoryOperations
			dataStateCount = i + 1
		}
	}
	bodyClassifier := wasm.NewModuleInstructionClassifier(m, true)
	validatedBodies := analysis.ValidFor(m)
	if validatedBodies {
		elemStateCount = max(elemStateCount, int(analysis.ElemStateCount))
		dataStateCount = max(dataStateCount, int(analysis.DataStateCount))
	}
	for functionIndex, fn := range m.Code {
		for _, local := range fn.Locals.Runs {
			out |= requiredFeaturesForValType(local.Type)
		}
		if validatedBodies && len(fn.BodyBytes) != 0 && analysis.Funcs[functionIndex].BodyBytes == uint32(len(fn.BodyBytes)) {
			bodyFacts := &analysis.Funcs[functionIndex]
			out |= requiredFeaturesForValidatedFlags(bodyFacts.Flags)
			if bodyFacts.Flags&wasm.ValidatedFuncUsesRefFunc != 0 {
				moduleFacts.UsesRefFunc = true
			}
			if bodyFacts.Flags&wasm.ValidatedFuncHasCallRef != 0 {
				moduleFacts.UsesCallRef = true
			}
			if bodyFacts.Flags&wasm.ValidatedFuncUsesAtomicWait != 0 {
				atomicWaitHelpers = true
			}
			needsExactScan := bodyFacts.Flags&wasm.ValidatedFuncNeedsDetailedRequirements != 0 ||
				(bodyFacts.Flags&wasm.ValidatedFuncUsesTableGrow != 0 && len(moduleFacts.TableGrowUsed) > 1) ||
				(bodyFacts.Flags&wasm.ValidatedFuncUsesMemoryGrow != 0 && len(moduleFacts.MemoryGrowUsed) > 1)
			if !needsExactScan {
				if bodyFacts.Flags&wasm.ValidatedFuncUsesTableGrow != 0 && len(moduleFacts.TableGrowUsed) == 1 {
					moduleFacts.TableGrowUsed[0] = true
				}
				if bodyFacts.Flags&wasm.ValidatedFuncUsesMemoryGrow != 0 && len(moduleFacts.MemoryGrowUsed) == 1 {
					moduleFacts.MemoryGrowUsed[0] = true
				}
				continue
			}
		}
		if len(fn.BodyBytes) != 0 {
			out |= requiredFeaturesAndSegmentCountsForBodyBytes(fn.BodyBytes, &elemStateCount, &dataStateCount, moduleFacts, &atomicWaitHelpers, m, &indexedFuncRefTest, &indexedFuncRefCast, &arm64GCRefTestHelper, &bodyClassifier)
		} else if len(fn.Body.Instrs) != 0 {
			programmaticCode = true
			instrsModuleRequirements(fn.Body.Instrs, &elemStateCount, &dataStateCount, moduleFacts, &atomicWaitHelpers)
		}
	}
	if programmaticCode && frontend.ModuleRequiresSIMD(m) {
		out |= CoreFeatureSIMD
	}
	return moduleRequirements{
		features:             out,
		elemStateCount:       elemStateCount,
		dataStateCount:       dataStateCount,
		moduleFacts:          moduleFacts,
		atomicWaitHelpers:    atomicWaitHelpers,
		indexedFuncRefTest:   indexedFuncRefTest,
		indexedFuncRefCast:   indexedFuncRefCast,
		arm64GCRefTestHelper: arm64GCRefTestHelper,
	}
}

func requiredFeaturesForValidatedFlags(flags wasm.ValidatedFuncFlags) CoreFeatures {
	var out CoreFeatures
	if flags&wasm.ValidatedFuncUsesBulkMemory != 0 {
		out |= CoreFeatureBulkMemoryOperations
	}
	if flags&wasm.ValidatedFuncUsesSaturatingTrunc != 0 {
		out |= CoreFeatureNonTrappingFloatToIntConversion
	}
	if flags&wasm.ValidatedFuncUsesReferenceTypes != 0 {
		out |= CoreFeatureReferenceTypes
	}
	if flags&wasm.ValidatedFuncUsesSignExtension != 0 {
		out |= CoreFeatureSignExtensionOps
	}
	if flags&wasm.ValidatedFuncUsesSIMD != 0 {
		out |= CoreFeatureSIMD
	}
	if flags&wasm.ValidatedFuncHasTailCall != 0 {
		out |= CoreFeatureTailCall
	}
	if flags&wasm.ValidatedFuncUsesTypedFunctionReferences != 0 {
		out |= CoreFeatureTypedFunctionReferences
	}
	if flags&wasm.ValidatedFuncUsesGC != 0 {
		out |= CoreFeatureGC
	}
	if flags&wasm.ValidatedFuncUsesExceptionHandling != 0 {
		out |= CoreFeatureExceptionHandling
	}
	if flags&wasm.ValidatedFuncUsesThreads != 0 {
		out |= CoreFeatureThreads
	}
	return out
}

func requiredFeaturesForConstExpr(expr wasm.Expr, importedGlobals int) CoreFeatures {
	features, _ := analyzeConstExprRequirements(expr, importedGlobals)
	return features
}

func analyzeConstExprRequirements(expr wasm.Expr, importedGlobals int) (CoreFeatures, bool) {
	body := expr.BodyBytes
	if len(body) == 0 {
		encoded, err := wasm.EncodeExpr(expr)
		if err != nil {
			return 0, true
		}
		body = encoded
	}
	return analyzeConstExprBytes(body, importedGlobals)
}

func requiredFeaturesForConstExprBytes(body []byte, importedGlobals int) CoreFeatures {
	features, _ := analyzeConstExprBytes(body, importedGlobals)
	return features
}

func analyzeConstExprBytes(body []byte, importedGlobals int) (CoreFeatures, bool) {
	r := wasm.NewReader(body)
	var out CoreFeatures
	usesRefFunc := false
	usesArithmetic, usesPriorLocal := false, false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return out, true
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return out, true
		}
		out |= requiredFeaturesForInstructionKind(imm.Kind)
		switch imm.Kind {
		case wasm.InstrRefFunc:
			usesRefFunc = true
		case wasm.InstrGlobalGet:
			if int(imm.Index) >= importedGlobals {
				usesPriorLocal = true
			}
		case wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32Mul,
			wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64Mul:
			usesArithmetic = true
		}
	}
	if usesPriorLocal {
		out |= CoreFeatureExtendedConstExpressions
	} else if usesArithmetic {
		out |= CoreFeatureExtendedConst
	}
	return out, usesRefFunc
}

func requiredFeaturesForValTypes(types []wasm.ValType) CoreFeatures {
	var out CoreFeatures
	for _, typ := range types {
		out |= requiredFeaturesForValType(typ)
	}
	return out
}

func requiredFeaturesForCompositeType(comp wasm.CompType) CoreFeatures {
	switch comp.Kind {
	case wasm.CompFunc:
		out := requiredFeaturesForValTypes(comp.Params) | requiredFeaturesForValTypes(comp.Results)
		if len(comp.Results) > 1 {
			out |= CoreFeatureMultiValue
		}
		return out
	case wasm.CompStruct:
		out := CoreFeatureGC
		for _, field := range comp.Fields {
			if storage := field.Storage(); !storage.Packed() {
				out |= requiredFeaturesForValType(storage.Val())
			}
		}
		return out
	case wasm.CompArray:
		out := CoreFeatureGC
		if storage := comp.Array.Storage(); !storage.Packed() {
			out |= requiredFeaturesForValType(storage.Val())
		}
		return out
	default:
		return 0
	}
}

func requiredFeaturesForTableRef(ref wasm.RefType) CoreFeatures {
	// A nullable funcref table is an MVP shape. Every other declared table
	// reference uses a post-MVP reference type and must survive artifact gating.
	if wasm.EqualValType(wasm.RefVal(ref), wasm.FuncRef) {
		return 0
	}
	return requiredFeaturesForValType(wasm.RefVal(ref))
}

func requiredFeaturesForValType(typ wasm.ValType) CoreFeatures {
	switch typ.Kind() {
	case wasm.ValRef:
		out := CoreFeatureReferenceTypes
		rt := typ.Ref()
		heap := rt.Heap()
		if heap.Kind() == wasm.HeapAbs {
			switch heap.Abs() {
			case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
				out |= CoreFeatureGC
			case wasm.HeapExn, wasm.HeapNoExn:
				out |= CoreFeatureExceptionHandling
			case wasm.HeapNoFunc, wasm.HeapNoExtern:
				out |= CoreFeatureGC
			}
		}
		if heap.Kind() == wasm.HeapTypeIndex || !rt.Nullable() || rt.Exact() {
			out |= CoreFeatureTypedFunctionReferences
		}
		return out
	case wasm.ValVec:
		if wasm.EqualValType(typ, wasm.V128) {
			return CoreFeatureSIMD
		}
	}
	return 0
}

func requiredFeaturesForBareValueTypeByte(encoded byte) (CoreFeatures, bool) {
	switch encoded {
	case 0x40, byte(wasm.NumI32), byte(wasm.NumI64), byte(wasm.NumF32), byte(wasm.NumF64):
		return 0, true
	case 0x7b:
		return CoreFeatureSIMD, true
	case byte(wasm.HeapExn), byte(wasm.HeapArray), byte(wasm.HeapStruct), byte(wasm.HeapI31),
		byte(wasm.HeapEq), byte(wasm.HeapAny), byte(wasm.HeapExtern), byte(wasm.HeapFunc),
		byte(wasm.HeapNone), byte(wasm.HeapNoExtern), byte(wasm.HeapNoFunc), byte(wasm.HeapNoExn):
		return requiredFeaturesForValType(wasm.RefVal(wasm.AbsRef(wasm.AbsHeapType(encoded)))), true
	default:
		return 0, false
	}
}

func requiredFeaturesForBodyBytes(body []byte) CoreFeatures {
	elemStateCount, dataStateCount := 0, 0
	return requiredFeaturesAndSegmentCountsForBodyBytes(body, &elemStateCount, &dataStateCount, nil, nil, nil, nil, nil, nil, nil)
}

func requiredFeaturesAndSegmentCountsForBodyBytes(body []byte, elemStateCount, dataStateCount *int, facts *frontend.ModuleFacts, atomicWaitHelpers *bool, m *wasm.Module, indexedFuncRefTest, indexedFuncRefCast, arm64GCRefTestHelper *bool, classifier *wasm.ModuleInstructionClassifier) CoreFeatures {
	var out CoreFeatures
	r := wasm.NewReader(body)
	if classifier == nil {
		local := wasm.NewModuleInstructionClassifier(m, true)
		classifier = &local
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			break
		}
		if kind, ok := wasm.ImmediateFreeInstructionKind(op); ok {
			out |= requiredFeaturesForInstructionKind(kind)
			continue
		}
		// Scalar constants and one-index control/local/global operations dominate
		// ordinary function bodies. The requirement pass only needs to consume
		// their immediate (and, for proposal opcodes, set a feature bit), so avoid
		// the general classifier and its reader adaptation on this hot path.
		switch op {
		case 0x05, 0x0b: // else, end
			continue
		case 0x0c, 0x0d, 0x10, 0x20, 0x21, 0x22, 0x23, 0x24: // br*, call, local.*, global.*
			if _, err := r.U32(); err != nil {
				return out
			}
			continue
		case 0x08: // throw
			if _, err := r.U32(); err != nil {
				return out
			}
			out |= CoreFeatureExceptionHandling
			continue
		case 0x12: // return_call
			if _, err := r.U32(); err != nil {
				return out
			}
			out |= CoreFeatureTailCall
			continue
		case 0x14, 0xd5, 0xd6: // call_ref, br_on_null, br_on_non_null
			if _, err := r.U32(); err != nil {
				return out
			}
			out |= CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
			if op == 0x14 && facts != nil {
				facts.UsesCallRef = true
			}
			continue
		case 0x15: // return_call_ref
			if _, err := r.U32(); err != nil {
				return out
			}
			out |= CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureTailCall
			if facts != nil {
				facts.UsesCallRef = true
			}
			continue
		case 0x25, 0x26, 0xd2: // table.get, table.set, ref.func
			if _, err := r.U32(); err != nil {
				return out
			}
			out |= CoreFeatureReferenceTypes
			if op == 0xd2 && facts != nil {
				facts.UsesRefFunc = true
			}
			continue
		case 0x41:
			if _, err := r.I32(); err != nil {
				return out
			}
			continue
		case 0x42:
			if _, err := r.I64(); err != nil {
				return out
			}
			continue
		case 0x43:
			if err := r.Step(4); err != nil {
				return out
			}
			continue
		case 0x44:
			if err := r.Step(8); err != nil {
				return out
			}
			continue
		}
		if op == 0xfd {
			out |= CoreFeatureSIMD
		}
		if op == 0x02 || op == 0x03 || op == 0x04 {
			first, err := r.Byte()
			if err != nil {
				break
			}
			if features, ok := requiredFeaturesForBareValueTypeByte(first); ok {
				out |= features
			} else if first == 0x63 || first == 0x64 {
				heap, readErr := r.S33()
				if readErr != nil {
					break
				}
				out |= requiredFeaturesForHeapImmediate(heap)
			} else {
				out |= CoreFeatureMultiValue
				for first&0x80 != 0 {
					first, err = r.Byte()
					if err != nil {
						break
					}
				}
			}
			continue
		}
		if op == 0xd0 {
			heap, readErr := r.S33()
			if readErr != nil {
				break
			}
			out |= requiredFeaturesForHeapImmediate(heap)
			continue
		}
		if op == 0x1c {
			n, readErr := r.U32()
			if readErr != nil {
				break
			}
			for i := uint32(0); i < n; i++ {
				b, readErr := r.Byte()
				if readErr != nil {
					break
				}
				if features, ok := requiredFeaturesForBareValueTypeByte(b); ok {
					out |= features
				} else if b == 0x63 || b == 0x64 {
					heap, heapErr := r.S33()
					if heapErr != nil {
						break
					}
					out |= requiredFeaturesForHeapImmediate(heap)
				}
			}
			continue
		}
		var probe wasm.Reader
		if op == 0xfb {
			probe = *r
		}
		var imm wasm.InstructionImmediate
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			break
		}
		segmentStateCount(imm.Kind, imm.Index, imm.Index2, elemStateCount, dataStateCount)
		recordModuleRequirementFact(imm.Kind, imm.Index, facts, atomicWaitHelpers)
		if op == 0xfb {
			recordRefTypeRequirements(m, imm.Kind, &probe, indexedFuncRefTest, indexedFuncRefCast, arm64GCRefTestHelper)
		}
		out |= requiredFeaturesForInstructionKind(imm.Kind)
		if imm.Kind == wasm.InstrCallIndirect && imm.Index2 != 0 {
			out |= CoreFeatureReferenceTypes
		}
	}
	return out
}

func recordRefTypeRequirements(m *wasm.Module, kind wasm.InstrKind, probe *wasm.Reader, refTest, refCast, arm64GCRefTestHelper *bool) {
	if m == nil || (kind != wasm.InstrRefTest && kind != wasm.InstrRefCast) {
		return
	}
	subopcode, err := probe.U32()
	if err != nil || (subopcode != 20 && subopcode != 21 && subopcode != 22 && subopcode != 23) {
		return
	}
	heap, err := probe.S33()
	if err != nil {
		return
	}
	if kind == wasm.InstrRefTest && (subopcode == 20 || subopcode == 21) && arm64GCRefTestHelper != nil {
		if heap >= 0 {
			if _, isFunc := m.TypeFunc(uint32(heap)); !isFunc {
				*arm64GCRefTestHelper = true
			}
		} else {
			switch heap {
			case -16, -17, -13, -14:
			default:
				*arm64GCRefTestHelper = true
			}
		}
	}
	if heap < 0 {
		return
	}
	if _, ok := m.TypeFunc(uint32(heap)); !ok {
		return
	}
	if kind == wasm.InstrRefTest {
		if refTest != nil {
			*refTest = true
		}
	} else if refCast != nil {
		*refCast = true
	}
}

func instrsModuleRequirements(instrs []wasm.Instruction, elemCount, dataCount *int, facts *frontend.ModuleFacts, atomicWaitHelpers *bool) {
	for i := range instrs {
		in := &instrs[i]
		segmentStateCount(in.Kind, in.Index, in.Index2, elemCount, dataCount)
		recordModuleRequirementFact(in.Kind, in.Index, facts, atomicWaitHelpers)
		instrsModuleRequirements(in.Body().Instrs, elemCount, dataCount, facts, atomicWaitHelpers)
		instrsModuleRequirements(in.Then(), elemCount, dataCount, facts, atomicWaitHelpers)
		instrsModuleRequirements(in.Else(), elemCount, dataCount, facts, atomicWaitHelpers)
	}
}

func recordModuleRequirementFact(kind wasm.InstrKind, index uint32, facts *frontend.ModuleFacts, atomicWaitHelpers *bool) {
	if atomicWaitHelpers != nil {
		switch kind {
		case wasm.InstrMemoryAtomicNotify, wasm.InstrMemoryAtomicWait32, wasm.InstrMemoryAtomicWait64:
			*atomicWaitHelpers = true
		}
	}
	if facts == nil {
		return
	}
	switch kind {
	case wasm.InstrTableGrow:
		if uint64(index) < uint64(len(facts.TableGrowUsed)) {
			facts.TableGrowUsed[index] = true
		}
	case wasm.InstrMemoryGrow:
		if uint64(index) < uint64(len(facts.MemoryGrowUsed)) {
			facts.MemoryGrowUsed[index] = true
		}
	case wasm.InstrRefFunc:
		facts.UsesRefFunc = true
	case wasm.InstrCallRef, wasm.InstrReturnCallRef:
		facts.UsesCallRef = true
	}
}

func requiredFeaturesForInstructionKind(kind wasm.InstrKind) CoreFeatures {
	if wasm.IsCoreAtomicInstructionKind(kind) {
		return CoreFeatureThreads
	}
	switch kind {
	case wasm.InstrI32Extend8S, wasm.InstrI32Extend16S, wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		return CoreFeatureSignExtensionOps
	case wasm.InstrMemoryInit, wasm.InstrMemoryCopy, wasm.InstrMemoryFill, wasm.InstrDataDrop,
		wasm.InstrTableInit, wasm.InstrElemDrop, wasm.InstrTableCopy:
		return CoreFeatureBulkMemoryOperations
	case wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableSize, wasm.InstrTableFill,
		wasm.InstrRefNull, wasm.InstrRefIsNull, wasm.InstrRefFunc, wasm.InstrRefEq:
		return CoreFeatureReferenceTypes
	case wasm.InstrCallRef, wasm.InstrRefAsNonNull, wasm.InstrBrOnNull, wasm.InstrBrOnNonNull:
		return CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
	case wasm.InstrRefI31, wasm.InstrI31GetS, wasm.InstrI31GetU,
		wasm.InstrRefTest, wasm.InstrRefCast, wasm.InstrBrOnCast, wasm.InstrBrOnCastFail,
		wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny:
		return CoreFeatureReferenceTypes | CoreFeatureGC
	case wasm.InstrReturnCall, wasm.InstrReturnCallIndirect:
		return CoreFeatureTailCall
	case wasm.InstrReturnCallRef:
		return CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureTailCall
	case wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrTryTable:
		return CoreFeatureExceptionHandling
	case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
		wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
		return CoreFeatureNonTrappingFloatToIntConversion
	default:
		return 0
	}
}

func requiredFeaturesForHeapImmediate(heap int64) CoreFeatures {
	out := CoreFeatureReferenceTypes
	switch heap {
	case -22, -21, -20, -19, -18, -15: // array / struct / i31 / eq / any / none
		out |= CoreFeatureGC
	case -23, -12: // exn / noexn
		out |= CoreFeatureExceptionHandling
	case -14, -13: // noextern / nofunc
		out |= CoreFeatureGC
	default:
		if heap >= 0 {
			out |= CoreFeatureTypedFunctionReferences
		}
	}
	return out
}

func compiledStructuralRequiredFeatures(c *Compiled) CoreFeatures {
	if c == nil {
		return 0
	}
	out := c.requiredFeatures
	if compiledMetadataUsesSIMD(c) {
		out |= CoreFeatureSIMD
	}
	out |= compiledAggregateStorageRequiredFeatures(c)
	for _, typ := range c.Types {
		if !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.HasDescriptor || typ.Kind == CompositeTypeStruct || typ.Kind == CompositeTypeArray {
			out |= CoreFeatureGC
		}
		switch typ.Kind {
		case CompositeTypeFunction:
			out |= requiredFeaturesForTypeDescriptors(typ.Params)
			out |= requiredFeaturesForTypeDescriptors(typ.Results)
			if len(typ.Results) > 1 {
				out |= CoreFeatureMultiValue
			}
		}
	}
	for _, sig := range c.importFuncSigs {
		if len(sig.Results) > 1 {
			out |= CoreFeatureMultiValue
		}
		out |= requiredFeaturesForPublicValTypes(sig.Params)
		out |= requiredFeaturesForPublicValTypes(sig.Results)
		if sig.HasTypeIndex && int(sig.TypeIndex) < len(c.Types) && c.Types[sig.TypeIndex].Kind == CompositeTypeFunction {
			out |= requiredFeaturesForTypeDescriptors(c.Types[sig.TypeIndex].Params)
			out |= requiredFeaturesForTypeDescriptors(c.Types[sig.TypeIndex].Results)
		}
	}
	for _, sig := range c.Funcs {
		if len(sig.Results) > 1 {
			out |= CoreFeatureMultiValue
		}
		out |= requiredFeaturesForPublicValTypes(sig.Params)
		out |= requiredFeaturesForPublicValTypes(sig.Results)
		if sig.HasTypeIndex && int(sig.TypeIndex) < len(c.Types) && c.Types[sig.TypeIndex].Kind == CompositeTypeFunction {
			out |= requiredFeaturesForTypeDescriptors(c.Types[sig.TypeIndex].Params)
			out |= requiredFeaturesForTypeDescriptors(c.Types[sig.TypeIndex].Results)
		}
	}
	for _, g := range c.GlobalImports {
		out |= requiredFeaturesForStoredType(c, g.Type, g.HasValueType, g.ValueTypeIndex, false)
		if g.Mutable {
			out |= CoreFeatureMutableGlobal
		}
	}
	for _, g := range c.Globals {
		out |= requiredFeaturesForConstExprBytes(g.InitExpr, len(c.GlobalImports))
		out |= requiredFeaturesForStoredType(c, g.Type, g.HasValueType, g.ValueTypeIndex, false)
	}
	for _, index := range c.GlobalExports {
		if index >= 0 && index < len(c.Globals) && c.Globals[index].Mutable {
			out |= CoreFeatureMutableGlobal
		}
	}
	if c.memoryCount() > 1 {
		out |= CoreFeatureMultiMemory
	}
	for i := 0; i < c.memoryCount(); i++ {
		memory := c.memoryDef(i)
		if memory.Addr64 {
			out |= CoreFeatureMemory64
		}
		if memory.Shared {
			out |= CoreFeatureThreads
		}
	}
	if c.hasExternrefTable() || c.tableCount() > 1 {
		out |= CoreFeatureReferenceTypes
	}
	for i := 0; i < c.tableCount(); i++ {
		def := c.tableDef(i)
		out |= requiredFeaturesForStoredType(c, c.tableElementType(i), def.HasValueType, def.ValueTypeIndex, true)
		if c.tableDef(i).Addr64 {
			out |= CoreFeatureTable64
		}
	}
	for _, elem := range c.Elems {
		out |= requiredFeaturesForConstExprBytes(elem.Offset.Expr, len(c.GlobalImports))
		out |= requiredFeaturesForStoredType(c, normalizedElemRefType(elem.RefType), elem.HasValueType, elem.ValueTypeIndex, !elem.HasValueType)
		if elem.RefType == ValExternRef || elem.TableIndex != 0 {
			out |= CoreFeatureReferenceTypes
		}
	}
	for _, elem := range c.passiveElems {
		out |= requiredFeaturesForConstExprBytes(elem.Offset.Expr, len(c.GlobalImports))
		out |= requiredFeaturesForStoredType(c, normalizedElemRefType(elem.RefType), elem.HasValueType, elem.ValueTypeIndex, !elem.HasValueType)
		if elem.RefType == ValExternRef {
			out |= CoreFeatureReferenceTypes
		}
		if elem.Mode != ElemModeActive {
			out |= CoreFeatureBulkMemoryOperations
		}
	}
	for _, data := range c.Data {
		out |= requiredFeaturesForConstExprBytes(data.Offset.Expr, len(c.GlobalImports))
	}
	return out
}

func compiledAggregateStorageRequiredFeatures(c *Compiled) CoreFeatures {
	if c == nil {
		return 0
	}
	var out CoreFeatures
	for _, typ := range c.Types {
		switch typ.Kind {
		case CompositeTypeStruct:
			for _, field := range typ.Fields {
				if !field.Storage.Packed {
					out |= requiredFeaturesForTypeDescriptor(field.Storage.Value)
				}
			}
		case CompositeTypeArray:
			if !typ.Array.Storage.Packed {
				out |= requiredFeaturesForTypeDescriptor(typ.Array.Storage.Value)
			}
		}
	}
	return out
}

func requiredFeaturesForStoredType(c *Compiled, abi ValType, hasExact bool, exactIndex uint32, mvpFuncref bool) CoreFeatures {
	if hasExact && uint64(exactIndex) < uint64(len(c.ValueTypes)) {
		exact := c.ValueTypes[exactIndex]
		if mvpFuncref && exact.Kind == ValueTypeReference && exact.Ref.Nullable && !exact.Ref.Exact && !exact.Ref.Heap.Defined && exact.Ref.Heap.Abstract == AbstractHeapFunc {
			return 0 // nullable funcref storage is an MVP shape
		}
		return requiredFeaturesForTypeDescriptor(exact)
	}
	if mvpFuncref && abi == ValFuncRef {
		return 0
	}
	return requiredFeaturesForPublicValType(abi)
}

func requiredFeaturesForTypeDescriptors(types []ValueTypeDescriptor) CoreFeatures {
	var out CoreFeatures
	for _, typ := range types {
		out |= requiredFeaturesForTypeDescriptor(typ)
	}
	return out
}

func requiredFeaturesForTypeDescriptor(typ ValueTypeDescriptor) CoreFeatures {
	if typ.Kind == ValueTypeV128 {
		return CoreFeatureSIMD
	}
	if typ.Kind != ValueTypeReference {
		return 0
	}
	out := CoreFeatureReferenceTypes
	if typ.Ref.Heap.Defined || !typ.Ref.Nullable || typ.Ref.Exact {
		out |= CoreFeatureTypedFunctionReferences
	}
	if !typ.Ref.Heap.Defined {
		switch typ.Ref.Heap.Abstract {
		case AbstractHeapAny, AbstractHeapEq, AbstractHeapI31, AbstractHeapStruct, AbstractHeapArray, AbstractHeapNone:
			out |= CoreFeatureGC
		case AbstractHeapExn, AbstractHeapNoExn:
			out |= CoreFeatureExceptionHandling
		case AbstractHeapNoFunc, AbstractHeapNoExtern:
			out |= CoreFeatureGC
		}
	}
	return out
}

func requiredFeaturesForPublicValTypes(types []ValType) CoreFeatures {
	var out CoreFeatures
	for _, typ := range types {
		out |= requiredFeaturesForPublicValType(typ)
	}
	return out
}

func requiredFeaturesForPublicValType(typ ValType) CoreFeatures {
	var out CoreFeatures
	if isReferenceValType(typ) {
		out |= CoreFeatureReferenceTypes
	}
	if typ == ValAnyRef || typ == ValI31Ref {
		out |= CoreFeatureGC
	}
	if typ == ValExnRef {
		out |= CoreFeatureExceptionHandling
	}
	if typ == ValV128 {
		out |= CoreFeatureSIMD
	}
	return out
}
