//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Primitive references use opaque full-width carriers. Actual descriptor
// targets carry exact primitive signatures; ordinary direct local calls
// may pass the validated nullable carriers through the existing frame ABI.
func admitPublicPrimitiveReferenceModule(m *wasm.Module, vector bool) bool {
	if m == nil || len(m.Code) > MaxPublicCodeBytes/16 || len(m.FuncTypes) != len(m.Code) || len(m.Imports) > MaxPublicImports || len(m.Types) > MaxPublicCodeBytes/8 || len(m.Tables)+m.ImportedTableCount() > MaxFunctions || len(m.Elements) > MaxPublicElementSegments || len(m.Tags) != 0 || len(m.StringRefs) != 0 {
		return false
	}
	directory, directoryErr := newSourceFunctionTypeDirectory(m)
	if directoryErr != nil {
		return false
	}
	types := internalReferenceTypes{decoded: m.Types, sourceDirectory: directory, publicReferences: true, decodedSWAR: vector}
	remaining := MaxPublicCodeBytes
	if chargeDeclaredTypeBytes(m, &remaining) != nil || admitSWARDecodedMetadataBudget(m, &remaining) != nil {
		return false
	}
	flat := uint32(0)
	opaqueAggregates := false
	for _, group := range m.Types {
		if len(group.SubTypes) > MaxPublicCodeBytes/8-int(flat) {
			return false
		}
		for _, sub := range group.SubTypes {
			if len(sub.Supers) > 1 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
				return false
			}
			if sub.Comp.Kind == wasm.CompStruct || sub.Comp.Kind == wasm.CompArray {
				opaqueAggregates = true
				flat++
				continue
			}
			if sub.Comp.Kind != wasm.CompFunc {
				return false
			}
			signature, ok := directory.function(flat)
			if !ok || !primitiveReferenceTargetWithTypes(signature, types, vector) {
				return false
			}
			flat++
		}
	}
	numeric := func(index uint32) bool {
		binding, ok := m.FuncTypeIndex(index)
		if !ok || binding.Rec {
			return false
		}
		sig, callable := directory.function(binding.Index)
		return callable && primitiveReferenceTargetWithTypes(sig, types, vector)
	}
	for _, imported := range m.Imports {
		switch imported.Type.Kind {
		case wasm.ExternFunc:
			index := imported.Type.FuncType()
			signature, ok := directory.function(index.Index)
			if index.Rec || !ok || !primitiveReferenceTargetWithTypes(signature, types, vector) {
				return false
			}
		case wasm.ExternGlobal:
			if value := imported.Type.GlobalType().Type; value.Kind() == wasm.ValRef && !types.publicFunctionReference(value) {
				return false
			}
		case wasm.ExternTable:
			if !types.publicFunctionReference(wasm.RefVal(imported.Type.TableType().Ref)) {
				return false
			}
		case wasm.ExternMem:
		default:
			return false
		}
	}
	for index, global := range m.Globals {
		if global.Type.Type.Kind() != wasm.ValRef {
			continue
		}
		if !admitPrimitiveReferenceGlobalInitializer(global, m, m.ImportedGlobalCount()+index) {
			return false
		}
		if wasm.EqualValType(global.Type.Type, wasm.FuncRef) && len(global.Init.BodyBytes) > 0 && global.Init.BodyBytes[0] == 0xd2 {
			value, err := tableElementExpression(global.Init)
			if err != nil || !numeric(value.FuncIndex) {
				return false
			}
		}
	}
	for _, table := range m.Tables {
		if !types.publicFunctionReference(wasm.RefVal(table.Type.Ref)) {
			return false
		}
		if table.Init != nil {
			value, err := elementInitializerExpression(*table.Init, m, wasm.ElemActive, true)
			if err != nil || !value.Null && !value.HasGlobal && !numeric(value.FuncIndex) {
				return false
			}
		}
	}
	for _, segment := range m.Elements {
		if segment.Kind.Kind == wasm.ElemFuncs {
			for _, index := range segment.Kind.Funcs {
				if !numeric(uint32(index)) {
					return false
				}
			}
			continue
		}
		if segment.Kind.Kind == wasm.ElemTypedExprs && !types.publicFunctionReference(wasm.RefVal(segment.Kind.Ref)) {
			return false
		}
		for _, expr := range segment.Kind.Exprs {
			value, err := elementInitializerExpression(expr, m, segment.Mode.Kind, true)
			if err != nil || !value.Null && !value.HasGlobal && !numeric(value.FuncIndex) {
				return false
			}
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for _, function := range m.Code {
		if len(function.BodyBytes) == 0 || len(function.Body.Instrs) != 0 || len(function.Body.BodyBytes) != 0 || len(function.BodyBytes) > remaining {
			return false
		}
		remaining -= len(function.BodyBytes)
		for _, run := range function.Locals.Runs {
			if run.Type.Kind() == wasm.ValRef && !types.publicFunctionReference(run.Type) {
				return false
			}
		}
		reader := wasm.ReaderFrom(function.BodyBytes)
		for reader.HasNext() {
			op, err := reader.Byte()
			if err != nil {
				return false
			}
			var immediate wasm.InstructionImmediate
			if classifier.ClassifyInto(&reader, op, &immediate) != nil || op == 0xd2 && !numeric(immediate.Index) {
				return false
			}
			if opaqueAggregates && op == 0xfb {
				if (immediate.Kind != wasm.InstrRefTest && immediate.Kind != wasm.InstrRefCast) || !immediate.HasHeapType || immediate.HeapType.Kind() != wasm.HeapTypeIndex || immediate.HeapType.Type().Rec {
					return false
				}
				if _, function := directory.function(immediate.HeapType.Type().Index); !function {
					return false
				}
			}
			if op == 0x11 || op == 0x13 || op == 0x14 || op == 0x15 {
				signature, ok := directory.function(immediate.Index)
				if !ok || !primitiveReferenceTargetWithTypes(signature, types, vector) {
					return false
				}
			}
		}
	}
	return true
}

func primitiveReferenceTable(typ wasm.RefType) bool {
	return typ == wasm.FuncRef.Ref() || typ == wasm.ExternRef.Ref()
}

func primitiveNumericReferenceTarget(sig *wasm.CompType, vector bool) bool {
	if vector {
		return swarCallSignature(sig) == nil
	}
	return integerNumericSignature(sig, true) == nil
}

func admitPrimitiveReferenceGlobalInitializer(global wasm.Global, m *wasm.Module, limit int) bool {
	return admitPrimitiveReferenceInitializer(global.Init, m, global.Type.Type, limit)
}

// AdmitPrimitiveReferenceGlobalInitializer shares the bounded initializer
// grammar with exact frame planning. It grants no native execution permission.
func AdmitPrimitiveReferenceGlobalInitializer(global wasm.Global, m *wasm.Module, limit int) bool {
	return admitPrimitiveReferenceGlobalInitializer(global, m, limit)
}

func admitPrimitiveReferenceInitializer(expr wasm.Expr, m *wasm.Module, expected wasm.ValType, limit int) bool {
	if !(internalReferenceTypes{publicReferences: true, sourceDirectory: sourceFunctionTypeDirectory{module: m}}).publicFunctionReference(expected) {
		return false
	}
	if len(expr.BodyBytes) > 0 && expr.BodyBytes[0] == 0x23 {
		return admitGlobalExpression(expr, m, expected, limit) == nil
	}
	if actual, ok := primitiveReferenceNullInitializer(expr, m); ok {
		return m.ReferenceTypeSubtype(actual, expected.Ref())
	}
	value, err := elementInitializerExpression(expr, m, wasm.ElemActive, true)
	if err != nil {
		return false
	}
	actual := value.RefType
	if !value.Null {
		index, ok := m.FuncTypeIndex(value.FuncIndex)
		if !ok || index.Rec {
			return false
		}
		actual = wasm.Ref(false, wasm.IndexedHeap(index), false)
	}
	return m.ReferenceTypeSubtype(actual, expected.Ref())
}

func primitiveReferenceNullInitializer(expr wasm.Expr, m *wasm.Module) (wasm.RefType, bool) {
	if m == nil || len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 || expr.BodyBytes[0] != 0xd0 {
		return wasm.RefType{}, false
	}
	r := wasm.ReaderFrom(expr.BodyBytes[1:])
	heap, err := r.S33()
	if err != nil {
		return wasm.RefType{}, false
	}
	var typ wasm.HeapType
	if heap >= 0 {
		typ = wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)})
	} else {
		switch heap {
		case -16, -13, -17, -14:
			typ = wasm.AbsHeap(wasm.AbsHeapType(heap + 128))
		default:
			return wasm.RefType{}, false
		}
	}
	end, err := r.Byte()
	ref := wasm.Ref(true, typ, false)
	types := internalReferenceTypes{publicReferences: true, sourceDirectory: sourceFunctionTypeDirectory{module: m}}
	return ref, err == nil && end == 0x0b && r.BytesLeft() == 0 && types.publicFunctionReference(wasm.RefVal(ref))
}

func primitiveReferenceTarget(sig *wasm.CompType, vector bool) bool {
	_, _, err := admitDeclaredControlSignature(sig, internalReferenceTypes{publicReferences: true}, true, false, vector)
	return err == nil
}

func primitiveReferenceTargetWithTypes(sig *wasm.CompType, types internalReferenceTypes, vector bool) bool {
	_, _, err := admitDeclaredControlSignature(sig, types, true, true, vector)
	return err == nil
}

func primitiveReferenceCarrierTarget(sig *wasm.CompType, types internalReferenceTypes) bool {
	references, _, err := admitDeclaredControlSignature(sig, types, true, true, false)
	return references && err == nil
}
