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
// dependencies of the compiled artifact. Codec version 4 and later store the
// full public CoreFeatures mask and reject unknown bits. Compile-time-only
// features such as extended constant expressions are folded into initializer
// metadata.
func moduleRequiredFeatures(m *wasm.Module) CoreFeatures {
	return analyzeModuleRequirements(m).features
}

func analyzeModuleRequirements(m *wasm.Module) moduleRequirements {
	return analyzeModuleRequirementsWithValidation(m, nil)
}

func requiredFeaturesForConstExprBytes(body []byte, importedGlobals int) CoreFeatures {
	features, _ := analyzeConstExprBytes(body, importedGlobals)
	return features
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
				if first == 0x64 {
					out |= CoreFeatureTypedFunctionReferences
				}
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
					if b == 0x64 {
						out |= CoreFeatureTypedFunctionReferences
					}
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
		if g.HasInitGlobal && g.InitGlobal >= len(c.GlobalImports) {
			out |= CoreFeatureExtendedConstExpressions
		}
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
		for _, value := range elem.Values {
			out |= requiredFeaturesForConstExprBytes(value.Expr, len(c.GlobalImports))
			if value.HasGlobal && int(value.GlobalIndex) >= len(c.GlobalImports) {
				out |= CoreFeatureExtendedConstExpressions
			}
		}
		out |= requiredFeaturesForStoredType(c, normalizedElemRefType(elem.RefType), elem.HasValueType, elem.ValueTypeIndex, !elem.HasValueType)
		if elem.RefType == ValExternRef || elem.TableIndex != 0 {
			out |= CoreFeatureReferenceTypes
		}
	}
	for _, elem := range c.passiveElems {
		out |= requiredFeaturesForConstExprBytes(elem.Offset.Expr, len(c.GlobalImports))
		for _, value := range elem.Values {
			out |= requiredFeaturesForConstExprBytes(value.Expr, len(c.GlobalImports))
			if value.HasGlobal && int(value.GlobalIndex) >= len(c.GlobalImports) {
				out |= CoreFeatureExtendedConstExpressions
			}
		}
		out |= requiredFeaturesForStoredType(c, normalizedElemRefType(elem.RefType), elem.HasValueType, elem.ValueTypeIndex, !elem.HasValueType)
		if elem.RefType == ValExternRef {
			out |= CoreFeatureReferenceTypes
		}
		if elem.Mode != ElemModeActive {
			out |= CoreFeatureBulkMemoryOperations
		}
	}
	for i := 0; i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		out |= requiredFeaturesForConstExprBytes(data.Offset.Expr, len(c.GlobalImports))
	}
	if len(c.PassiveData) != 0 {
		out |= CoreFeatureBulkMemoryOperations
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
