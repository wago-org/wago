package wago

import (
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type moduleRequirements struct {
	features       CoreFeatures
	elemStateCount int
	dataStateCount int
}

// moduleRequiredFeatures records optional core features that remain execution
// dependencies of the compiled artifact. Codec v25 stores the full public
// CoreFeatures mask and rejects unknown bits. Compile-time-only features such as
// extended constant expressions are folded into initializer metadata.
func moduleRequiredFeatures(m *wasm.Module) CoreFeatures {
	return analyzeModuleRequirements(m).features
}

func analyzeModuleRequirements(m *wasm.Module) moduleRequirements {
	if m == nil {
		return moduleRequirements{}
	}
	var out CoreFeatures
	programmaticCode := false
	elemStateCount, dataStateCount := 0, 0
	if frontend.ModuleNonCodeRequiresSIMD(m) {
		out |= CoreFeatureSIMD
	}
	if m.TagCount() != 0 {
		out |= CoreFeatureExceptionHandling
	}

	for _, rec := range m.Types {
		for _, sub := range rec.SubTypes {
			if sub.HasPrefix || len(sub.Supers) != 0 {
				out |= CoreFeatureGC
			}
			if sub.Comp.Kind != wasm.CompFunc {
				out |= CoreFeatureGC
				continue
			}
			if len(sub.Comp.Results) > 1 {
				out |= CoreFeatureMultiValue
			}
			out |= requiredFeaturesForValTypes(sub.Comp.Params)
			out |= requiredFeaturesForValTypes(sub.Comp.Results)
		}
	}
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternGlobal:
			out |= requiredFeaturesForValType(im.Type.Global.Type)
			if im.Type.Global.Mutable {
				out |= CoreFeatureMutableGlobal
			}
		case wasm.ExternTable:
			if wasm.EqualValType(wasm.RefVal(im.Type.Table.Ref), wasm.ExternRef) {
				out |= CoreFeatureReferenceTypes
			}
			if im.Type.Table.Limits.Addr64 {
				out |= CoreFeatureTable64
			}
		}
	}
	for _, g := range m.Globals {
		out |= requiredFeaturesForValType(g.Type.Type)
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind == wasm.ExternGlobal {
			if gt, ok := m.GlobalTypeByIndex(uint32(ex.Index.Index)); ok && gt.Mutable {
				out |= CoreFeatureMutableGlobal
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
		if im.Type.Kind == wasm.ExternMem && im.Type.Mem.Limits.Addr64 {
			out |= CoreFeatureMemory64
		}
	}
	for _, memory := range m.Memories {
		if memory.Limits.Addr64 {
			out |= CoreFeatureMemory64
		}
	}
	for _, table := range m.Tables {
		if wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.ExternRef) || table.Init != nil {
			out |= CoreFeatureReferenceTypes
		}
		if table.Type.Limits.Addr64 {
			out |= CoreFeatureTable64
		}
	}
	for i, elem := range m.Elements {
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
		if data.Mode.Kind == wasm.DataPassive {
			out |= CoreFeatureBulkMemoryOperations
			dataStateCount = i + 1
		}
	}
	for _, fn := range m.Code {
		for _, local := range fn.Locals.Runs {
			out |= requiredFeaturesForValType(local.Type)
		}
		if len(fn.BodyBytes) != 0 {
			out |= requiredFeaturesAndSegmentCountsForBodyBytes(fn.BodyBytes, &elemStateCount, &dataStateCount)
		} else if len(fn.Body.Instrs) != 0 {
			programmaticCode = true
			instrsSegmentStateCounts(fn.Body.Instrs, &elemStateCount, &dataStateCount)
		}
	}
	if programmaticCode && frontend.ModuleRequiresSIMD(m) {
		out |= CoreFeatureSIMD
	}
	return moduleRequirements{features: out, elemStateCount: elemStateCount, dataStateCount: dataStateCount}
}

func requiredFeaturesForValTypes(types []wasm.ValType) CoreFeatures {
	var out CoreFeatures
	for _, typ := range types {
		out |= requiredFeaturesForValType(typ)
	}
	return out
}

func requiredFeaturesForValType(typ wasm.ValType) CoreFeatures {
	switch typ.Kind {
	case wasm.ValRef:
		out := CoreFeatureReferenceTypes
		if typ.Ref.Heap.Kind == wasm.HeapAbs {
			switch typ.Ref.Heap.Abs {
			case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
				out |= CoreFeatureGC
			case wasm.HeapExn, wasm.HeapNoExn:
				out |= CoreFeatureExceptionHandling
			}
		}
		if typ.Ref.Heap.Kind == wasm.HeapTypeIndex || !typ.Ref.Nullable || typ.Ref.Exact {
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

func requiredFeaturesForBodyBytes(body []byte) CoreFeatures {
	elemStateCount, dataStateCount := 0, 0
	return requiredFeaturesAndSegmentCountsForBodyBytes(body, &elemStateCount, &dataStateCount)
}

func requiredFeaturesAndSegmentCountsForBodyBytes(body []byte, elemStateCount, dataStateCount *int) CoreFeatures {
	var out CoreFeatures
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			break
		}
		if op == 0xfd {
			out |= CoreFeatureSIMD
		}
		if op == 0x02 || op == 0x03 || op == 0x04 {
			first, err := r.Byte()
			if err != nil {
				break
			}
			switch first {
			case 0x40, 0x7f, 0x7e, 0x7d, 0x7c:
			case 0x7b:
				out |= CoreFeatureSIMD
			case 0x70, 0x6f:
				out |= CoreFeatureReferenceTypes
			case 0x6e, 0x71:
				out |= CoreFeatureReferenceTypes | CoreFeatureGC
			case 0x69, 0x74:
				out |= CoreFeatureReferenceTypes | CoreFeatureExceptionHandling
			case 0x63, 0x64:
				heap, readErr := r.S33()
				if readErr != nil {
					break
				}
				out |= requiredFeaturesForHeapImmediate(heap)
			default:
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
				if b == 0x7b {
					out |= CoreFeatureSIMD
				}
				if b == 0x63 || b == 0x64 {
					heap, heapErr := r.S33()
					if heapErr != nil {
						break
					}
					out |= requiredFeaturesForHeapImmediate(heap)
				}
			}
			continue
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			break
		}
		segmentStateCount(imm.Kind, imm.Index, imm.Index2, elemStateCount, dataStateCount)
		switch imm.Kind {
		case wasm.InstrI32Extend8S, wasm.InstrI32Extend16S, wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
			out |= CoreFeatureSignExtensionOps
		case wasm.InstrMemoryInit, wasm.InstrMemoryCopy, wasm.InstrMemoryFill, wasm.InstrDataDrop,
			wasm.InstrTableInit, wasm.InstrElemDrop, wasm.InstrTableCopy:
			out |= CoreFeatureBulkMemoryOperations
		case wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableSize, wasm.InstrTableFill,
			wasm.InstrRefNull, wasm.InstrRefIsNull, wasm.InstrRefFunc, wasm.InstrRefEq:
			out |= CoreFeatureReferenceTypes
		case wasm.InstrCallRef, wasm.InstrRefAsNonNull, wasm.InstrBrOnNull, wasm.InstrBrOnNonNull:
			out |= CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
		case wasm.InstrReturnCall, wasm.InstrReturnCallIndirect:
			out |= CoreFeatureTailCall
		case wasm.InstrReturnCallRef:
			out |= CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureTailCall
		case wasm.InstrThrow, wasm.InstrThrowRef, wasm.InstrTryTable:
			out |= CoreFeatureExceptionHandling
		case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
			wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
			out |= CoreFeatureNonTrappingFloatToIntConversion
		}
		if imm.Kind == wasm.InstrCallIndirect && imm.Index2 != 0 {
			out |= CoreFeatureReferenceTypes
		}
	}
	return out
}

func requiredFeaturesForHeapImmediate(heap int64) CoreFeatures {
	out := CoreFeatureReferenceTypes
	switch heap {
	case -18, -15: // any / none
		out |= CoreFeatureGC
	case -23, -12: // exn / noexn
		out |= CoreFeatureExceptionHandling
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
	out |= requiredFeaturesForTypeDescriptors(c.ValueTypes)
	for _, typ := range c.Types {
		if !typ.Final || len(typ.Supers) != 0 || typ.Kind == CompositeTypeStruct || typ.Kind == CompositeTypeArray {
			out |= CoreFeatureGC
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
		if isReferenceValType(g.Type) {
			out |= CoreFeatureReferenceTypes
		}
		if g.Mutable {
			out |= CoreFeatureMutableGlobal
		}
	}
	for _, g := range c.Globals {
		if isReferenceValType(g.Type) {
			out |= CoreFeatureReferenceTypes
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
		if c.memoryDef(i).Addr64 {
			out |= CoreFeatureMemory64
		}
	}
	if c.hasExternrefTable() || c.tableCount() > 1 || c.NeedsFuncRefDescs {
		out |= CoreFeatureReferenceTypes
	}
	for i := 0; i < c.tableCount(); i++ {
		if c.tableDef(i).Addr64 {
			out |= CoreFeatureTable64
		}
	}
	for _, elem := range c.Elems {
		if elem.RefType == ValExternRef || elem.TableIndex != 0 {
			out |= CoreFeatureReferenceTypes
		}
	}
	for _, elem := range c.passiveElems {
		if elem.RefType == ValExternRef {
			out |= CoreFeatureReferenceTypes
		}
		if elem.Mode != ElemModeActive {
			out |= CoreFeatureBulkMemoryOperations
		}
	}
	return out
}

func requiredFeaturesForTypeDescriptors(types []ValueTypeDescriptor) CoreFeatures {
	var out CoreFeatures
	for _, typ := range types {
		if typ.Kind == ValueTypeV128 {
			out |= CoreFeatureSIMD
		}
		if typ.Kind != ValueTypeReference {
			continue
		}
		out |= CoreFeatureReferenceTypes
		if typ.Ref.Heap.Defined || !typ.Ref.Nullable || typ.Ref.Exact {
			out |= CoreFeatureTypedFunctionReferences
		}
		if !typ.Ref.Heap.Defined {
			switch typ.Ref.Heap.Abstract {
			case AbstractHeapAny, AbstractHeapEq, AbstractHeapI31, AbstractHeapStruct, AbstractHeapArray, AbstractHeapNone:
				out |= CoreFeatureGC
			case AbstractHeapExn, AbstractHeapNoExn:
				out |= CoreFeatureExceptionHandling
			}
		}
	}
	return out
}

func requiredFeaturesForPublicValTypes(types []ValType) CoreFeatures {
	var out CoreFeatures
	for _, typ := range types {
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
	}
	return out
}
