package wago

import (
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

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
			moduleFacts.UsesRefFunc = moduleFacts.UsesRefFunc || elem.Mode.Kind != wasm.ElemDeclarative && usesRefFunc
		}
		// Legacy declarations have no runtime values. Keep their feature bits
		// below while avoiding an otherwise unused per-function descriptor arena.
		moduleFacts.UsesRefFunc = moduleFacts.UsesRefFunc || (elem.Mode.Kind != wasm.ElemDeclarative && elem.Kind.Kind == wasm.ElemFuncs && len(elem.Kind.Funcs) != 0)
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
	for i := 0; i < m.DataSegmentCount(); i++ {
		data := m.DataSegment(i)
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
		elemStateCount = max(elemStateCount, int(analysis.ElemStateCount()))
		dataStateCount = max(dataStateCount, int(analysis.DataStateCount()))
	}
	for functionIndex, fn := range m.Code {
		for _, local := range fn.Locals.Runs {
			out |= requiredFeaturesForValType(local.Type)
		}
		if validatedBodies && len(fn.BodyBytes) != 0 && analysis.Func(functionIndex).BodyBytes == uint32(len(fn.BodyBytes)) {
			bodyFacts := analysis.Func(functionIndex)
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
	for index, used := range moduleFacts.MemoryGrowUsed {
		if !used {
			continue
		}
		if mt, ok := m.MemoryType(uint32(index)); ok && mt.Shared {
			atomicWaitHelpers = true
			break
		}
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
	if flags&wasm.ValidatedFuncUsesMultiValue != 0 {
		out |= CoreFeatureMultiValue
	}
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
		if op == 0xd0 {
			probe := *r
			if heap, err := probe.S33(); err == nil {
				out |= requiredFeaturesForHeapImmediate(heap)
			}
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return out, true
		}
		out |= requiredFeaturesForInstructionKind(imm.Kind)
		switch imm.Kind {
		case wasm.InstrRefFunc:
			usesRefFunc = true
		case wasm.InstrStructNew, wasm.InstrArrayNew, wasm.InstrArrayNewDefault,
			wasm.InstrAnyConvertExtern, wasm.InstrExternConvertAny:
			out |= CoreFeatureReferenceTypes | CoreFeatureGC
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

func requiredFeaturesForInstructionKind(kind wasm.InstrKind) CoreFeatures {
	if kind >= wasm.InstrI64Add128 && kind <= wasm.InstrI64MulWideU {
		return CoreFeatureWideArithmetic
	}
	if wasm.IsCoreAtomicInstructionKind(kind) {
		return CoreFeatureThreads
	}
	switch kind {
	case wasm.InstrI32Extend8S, wasm.InstrI32Extend16S, wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		return CoreFeatureSignExtensionOps
	case wasm.InstrMemoryInit, wasm.InstrMemoryCopy, wasm.InstrMemoryFill, wasm.InstrDataDrop:
		return CoreFeatureBulkMemoryOperations
	case wasm.InstrTableInit, wasm.InstrElemDrop, wasm.InstrTableCopy:
		return CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes
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
