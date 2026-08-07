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
		if len(rec.SubTypes) != 1 {
			// Empty and multi-member rec groups use the standardized recursive
			// type-section grammar even when all members are plain functions.
			out |= CoreFeatureTypedFunctionReferences
		}
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
		out |= requiredFeaturesForConstExpr(g.Init, m.ImportedGlobalCount())
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
		if im.Type.Kind == wasm.ExternMem {
			if im.Type.Mem.Limits.Addr64 {
				out |= CoreFeatureMemory64
			}
			if im.Type.Mem.Shared {
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
		if wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.ExternRef) || table.Init != nil {
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
		for _, expr := range elem.Kind.Exprs {
			out |= requiredFeaturesForConstExpr(expr, m.ImportedGlobalCount())
		}
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

func requiredFeaturesForConstExpr(expr wasm.Expr, importedGlobals int) CoreFeatures {
	body := expr.BodyBytes
	if len(body) == 0 {
		encoded, err := wasm.EncodeExpr(expr)
		if err != nil {
			return 0
		}
		body = encoded
	}
	return requiredFeaturesForConstExprBytes(body, importedGlobals)
}

func requiredFeaturesForConstExprBytes(body []byte, importedGlobals int) CoreFeatures {
	r := wasm.NewReader(body)
	var out CoreFeatures
	usesArithmetic, usesPriorLocal := false, false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			break
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			break
		}
		out |= requiredFeaturesForInstructionKind(imm.Kind)
		switch imm.Kind {
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
	return out
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

func moduleUsesAtomicWaitHelpers(m *wasm.Module) bool {
	for i := range m.Code {
		r := wasm.NewReader(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				break
			}
			imm, err := wasm.ClassifyInstructionImmediate(r, op)
			if err != nil {
				break
			}
			switch imm.Kind {
			case wasm.InstrMemoryAtomicNotify, wasm.InstrMemoryAtomicWait32, wasm.InstrMemoryAtomicWait64:
				return true
			}
		}
	}
	return false
}

func requiredFeaturesAndSegmentCountsForBodyBytes(body []byte, elemStateCount, dataStateCount *int) CoreFeatures {
	var out CoreFeatures
	r := wasm.NewReader(body)
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
			continue
		case 0x15: // return_call_ref
			if _, err := r.U32(); err != nil {
				return out
			}
			out |= CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureTailCall
			continue
		case 0x25, 0x26, 0xd2: // table.get, table.set, ref.func
			if _, err := r.U32(); err != nil {
				return out
			}
			out |= CoreFeatureReferenceTypes
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
		out |= requiredFeaturesForInstructionKind(imm.Kind)
		if imm.Kind == wasm.InstrCallIndirect && imm.Index2 != 0 {
			out |= CoreFeatureReferenceTypes
		}
	}
	return out
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
		out |= requiredFeaturesForConstExprBytes(g.InitExpr, len(c.GlobalImports))
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
		memory := c.memoryDef(i)
		if memory.Addr64 {
			out |= CoreFeatureMemory64
		}
		if memory.Shared {
			out |= CoreFeatureThreads
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
		out |= requiredFeaturesForConstExprBytes(elem.Offset.Expr, len(c.GlobalImports))
		if elem.RefType == ValExternRef || elem.TableIndex != 0 {
			out |= CoreFeatureReferenceTypes
		}
	}
	for _, elem := range c.passiveElems {
		out |= requiredFeaturesForConstExprBytes(elem.Offset.Expr, len(c.GlobalImports))
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
