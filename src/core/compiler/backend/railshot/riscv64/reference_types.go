//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A borrowed directory reuses the signatures admitted by the module profile.
// Exactly one view is used; resolving an annotation never allocates or converts
// the scalar width directory into a second type representation.
type internalReferenceTypes struct {
	scalar            []functionSpec
	swar              []*wasm.CompType
	decoded           []wasm.RecType
	decodedSWAR       bool
	declarationExtern bool
	// This exact execution permission admits nullable abstract extern carriers
	// in function signatures and locals. Explicit block/select annotations must
	// never inherit it.
	executionExtern bool
	// This permission is separate from declarationExtern because it applies to
	// an exact non-null function parameter, never block/select annotations.
	declarationNonNullExtern bool
	// The exact public reference-call boundary has one indexed callable type
	// whose signature itself carries abstract funcref or externref values. The
	// whole-module classifier prevents this from widening other declarations or
	// executable bodies.
	publicReferenceCarriers bool
	// i31 is confined to the exact non-allocating Core 3 product. It admits the
	// abstract i31 heap as one full-width internal immediate carrier.
	i31 bool
	// gcArrayFixedOfficial admits only the two indexed f32 array heaps and the
	// abstract array supertype in the pinned official fixed-array product.
	gcArrayFixedOfficial bool
	// gcArrayPackedData has the same two indexed-array and abstract-array
	// carrier boundary, independently admitted for the packed data product.
	gcArrayPackedData bool
	// gcArrayReference admits the five indexed reference-array heaps and the
	// abstract array carrier in the pinned official element-segment product.
	gcArrayReference bool
	// gcArrayInitElem admits the two indexed array heaps used by the pinned
	// official funcref element-initialization product.
	gcArrayInitElem bool
	// gcArrayInitData admits the bounded indexed array heaps in the pinned
	// official packed and wide data-initialization products.
	gcArrayInitData bool
	// gcArrayFill admits the two indexed packed-array heaps in the pinned
	// official bulk-fill product.
	gcArrayFill bool
	// gcArrayCopy admits the two indexed packed-array heaps in the pinned
	// official bulk-copy product.
	gcArrayCopy bool
	// gcStructNull admits the single indexed struct heap in the official
	// null-dereference product.
	gcStructNull bool
	// gcStructMutation admits the single nullable indexed struct local in the
	// pinned numeric struct.get/struct.set product.
	gcStructMutation bool
	// gcRefTestTable admits only the three indexed struct heaps in the pinned
	// rooted object-table ref.test product.
	gcRefTestTable bool
	// gcRefTestConcrete admits the eight indexed struct heaps in the official
	// concrete subtyping ref.test product.
	gcRefTestConcrete bool
	// gcRefTestAbstract admits the exact mixed abstract-reference products,
	// including branch-cast block signatures and nullable externref parameters.
	gcRefTestAbstract bool
	// gcBrOnCastNullability additionally admits abstract anyref carriers and the
	// single indexed empty-struct heap in the exact nullability products.
	gcBrOnCastNullability             bool
	gcStructPacked                    bool
	gcStructBasic                     bool
	gcTypeSubtypingRecursiveFunctions bool
	gcTypeSubtypingRefFuncGlobals     bool
	gcTypeSubtypingRuntimeCallCast    bool
	gcTypeSubtypingFinalityCallCast   bool
	gcTypeSubtypingRefTestSingle      bool
	gcTypeSubtypingRefTestVariant     uint8
	gcTypeSubtypingDuplicateRecursive bool
	// gcRootedScalar admits indexed heaps that name one of the
	// bounded numeric scalar structs authenticated by the whole-module generic profile.
	gcRootedScalar bool
}

func (types internalReferenceTypes) rootedStruct(index uint32) bool {
	if !types.gcRootedScalar {
		return false
	}
	signature := types.rootedStructType(index)
	if signature == nil || len(signature.Fields) == 0 || len(signature.Fields) > 255 {
		return false
	}
	for _, field := range signature.Fields {
		storage := field.Storage()
		if storage.Packed() {
			if storage.Pack() != wasm.PackI8 && storage.Pack() != wasm.PackI16 {
				return false
			}
		} else if !scalarType(storage.Val(), true) && !types.rootedCollectorReference(storage, index) {
			return false
		}
	}
	return true
}

func (types internalReferenceTypes) rootedCollectorReference(storage wasm.StorageType, owner uint32) bool {
	if storage.Packed() || storage.Val().Kind() != wasm.ValRef {
		return false
	}
	ref := storage.Val().Ref()
	heap := ref.Heap()
	if ref.Exact() || heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec || heap.Type().Index >= owner {
		return false
	}
	target := heap.Type().Index
	return types.rootedStruct(target) || types.rootedArray(target)
}

func (types internalReferenceTypes) rootedStructFieldReference(typeIndex, fieldIndex uint32) bool {
	storage, ok := types.rootedStructFieldStorage(typeIndex, fieldIndex)
	return ok && types.rootedCollectorReference(storage, typeIndex)
}

func (types internalReferenceTypes) rootedStructDefaultable(index uint32) bool {
	fields, ok := types.rootedStructFields(index)
	if !ok {
		return false
	}
	for fieldIndex, field := range fields {
		if types.rootedStructFieldReference(index, uint32(fieldIndex)) && !field.Storage().Val().Ref().Nullable() {
			return false
		}
	}
	return true
}

func (types internalReferenceTypes) rootedArray(index uint32) bool {
	if !types.gcRootedScalar {
		return false
	}
	signature := types.rootedAggregateType(index, wasm.CompArray)
	if signature == nil {
		return false
	}
	storage := signature.Array.Storage()
	if storage.Packed() {
		return storage.Pack() == wasm.PackI8 || storage.Pack() == wasm.PackI16
	}
	return scalarType(storage.Val(), true) || types.rootedCollectorReference(storage, index)
}

func (types internalReferenceTypes) rootedArrayElementReference(index uint32) bool {
	storage, _, ok := types.rootedArrayStorage(index)
	return ok && types.rootedCollectorReference(storage, index)
}

func (types internalReferenceTypes) rootedArrayDefaultable(index uint32) bool {
	storage, _, ok := types.rootedArrayStorage(index)
	return ok && (!types.rootedArrayElementReference(index) || storage.Val().Ref().Nullable())
}

func (types internalReferenceTypes) rootedAggregateType(index uint32, kind wasm.CompTypeKind) *wasm.CompType {
	var signature *wasm.CompType
	if types.scalar != nil {
		if uint64(index) >= uint64(len(types.scalar)) {
			return nil
		}
		signature = types.scalar[index].signature
	} else {
		if uint64(index) >= uint64(len(types.decoded)) || len(types.decoded[index].SubTypes) != 1 {
			return nil
		}
		signature = &types.decoded[index].SubTypes[0].Comp
	}
	if signature == nil || signature.Kind != kind {
		return nil
	}
	return signature
}

func (types internalReferenceTypes) rootedStructType(index uint32) *wasm.CompType {
	return types.rootedAggregateType(index, wasm.CompStruct)
}

func (types internalReferenceTypes) rootedArrayStorage(index uint32) (wasm.StorageType, bool, bool) {
	signature := types.rootedAggregateType(index, wasm.CompArray)
	if !types.rootedArray(index) {
		return wasm.StorageType{}, false, false
	}
	return signature.Array.Storage(), signature.Array.Mut() == wasm.Var, true
}

func (types internalReferenceTypes) rootedStructFields(index uint32) ([]wasm.FieldType, bool) {
	signature := types.rootedStructType(index)
	if !types.rootedStruct(index) {
		return nil, false
	}
	return signature.Fields, true
}

func (types internalReferenceTypes) rootedStructFieldStorage(typeIndex, fieldIndex uint32) (wasm.StorageType, bool) {
	signature := types.rootedStructType(typeIndex)
	if !types.rootedStruct(typeIndex) || uint64(fieldIndex) >= uint64(len(signature.Fields)) {
		return wasm.StorageType{}, false
	}
	return signature.Fields[fieldIndex].Storage(), true
}

func (types internalReferenceTypes) rootedStructField(typeIndex, fieldIndex uint32) bool {
	signature := types.rootedStructType(typeIndex)
	return types.rootedStruct(typeIndex) && uint64(fieldIndex) < uint64(len(signature.Fields))
}

func (types internalReferenceTypes) mutableStructField(typeIndex, fieldIndex uint32) bool {
	if !types.rootedStructField(typeIndex, fieldIndex) {
		return false
	}
	signature := types.rootedStructType(typeIndex)
	return signature.Fields[fieldIndex].Mut() == wasm.Var
}

func (types internalReferenceTypes) function(index uint32) bool {
	var signature *wasm.CompType
	vector := types.swar != nil || types.decodedSWAR
	if types.scalar != nil {
		if types.swar != nil || types.decoded != nil || types.decodedSWAR || len(types.scalar) > MaxFunctions || uint64(index) >= uint64(len(types.scalar)) {
			return false
		}
		signature = types.scalar[index].signature
	} else if types.swar != nil {
		if types.decoded != nil || types.decodedSWAR || len(types.swar) > MaxFunctions || uint64(index) >= uint64(len(types.swar)) {
			return false
		}
		signature = types.swar[index]
	} else {
		if len(types.decoded) > MaxFunctions || uint64(index) >= uint64(len(types.decoded)) {
			return false
		}
		group := &types.decoded[index]
		if len(group.SubTypes) != 1 {
			return false
		}
		sub := &group.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
			return false
		}
		signature = &sub.Comp
	}
	limit := 256
	if vector {
		limit = maxSWARLocals
	}
	if signature == nil || signature.Kind != wasm.CompFunc || len(signature.Params) > limit || len(signature.Results) > limit {
		return false
	}
	if types.publicReferenceCarriers && publicReferenceVectorCarrierTarget(signature) {
		return true
	}
	if types.gcRefTestAbstract && (index == 5 || index == 6) {
		return true
	}
	for _, list := range [][]wasm.ValType{signature.Params, signature.Results} {
		for _, typ := range list {
			if !vector && !scalarType(typ, true) || vector && !swarCarrierType(typ) {
				return false
			}
		}
	}
	return true
}

// The frontend proves initialization, nullability and matching function types
// before lowering erases these annotations to one full-width carrier class.
func internalFunctionReferenceWithTypes(typ wasm.ValType, typed bool, types internalReferenceTypes) bool {
	if typ.Kind() != wasm.ValRef {
		return false
	}
	ref := typ.Ref()
	if ref.Exact() && !types.gcStructBasic || !ref.Nullable() && !typed {
		return false
	}
	heap := ref.Heap()
	if types.gcRootedScalar && heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && (types.rootedStruct(heap.Type().Index) || types.rootedArray(heap.Type().Index)) {
		return true
	}
	if types.gcStructBasic && heap == wasm.AbsHeap(wasm.HeapAny) {
		return true
	}
	if (types.gcTypeSubtypingRecursiveFunctions || types.gcTypeSubtypingRefFuncGlobals || types.gcTypeSubtypingRuntimeCallCast || types.gcTypeSubtypingRefTestSingle) && heap.Kind() == wasm.HeapTypeIndex {
		return true
	}
	if types.gcTypeSubtypingDuplicateRecursive && (heap == wasm.AbsHeap(wasm.HeapFunc) || heap.Kind() == wasm.HeapTypeIndex) {
		return true
	}
	if types.i31 && heap == wasm.AbsHeap(wasm.HeapI31) {
		return true
	}
	if types.gcStructNull && (heap == wasm.AbsHeap(wasm.HeapStruct) || heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && heap.Type().Index == 0) {
		return true
	}
	if (types.gcStructMutation || types.gcStructPacked || types.gcStructBasic) && (heap == wasm.AbsHeap(wasm.HeapStruct) || heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && heap.Type().Index == 0) {
		return true
	}
	if types.gcRefTestTable && heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && heap.Type().Index <= 2 {
		return true
	}
	if types.gcRefTestConcrete && (heap == wasm.AbsHeap(wasm.HeapStruct) || heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && heap.Type().Index < 8) {
		return true
	}
	if types.gcBrOnCastNullability && heap == wasm.AbsHeap(wasm.HeapAny) {
		return true
	}
	if types.gcRefTestAbstract {
		if heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && (heap.Type().Index == 1 || heap.Type().Index == 2) {
			return true
		}
		if heap.Kind() == wasm.HeapAbs {
			switch heap.Abs() {
			case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray,
				wasm.HeapNone, wasm.HeapFunc, wasm.HeapNoFunc, wasm.HeapExtern, wasm.HeapNoExtern:
				return true
			}
		}
	}
	if types.gcArrayFixedOfficial || types.gcArrayPackedData || types.gcArrayReference || types.gcArrayInitElem || types.gcArrayInitData || types.gcArrayFill || types.gcArrayCopy {
		if heap == wasm.AbsHeap(wasm.HeapArray) {
			return true
		}
		limit := uint32(2)
		if types.gcArrayReference {
			limit = 5
		}
		if types.gcArrayInitElem {
			limit = 3
		}
		if types.gcArrayInitData {
			limit = 3
		}
		if heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && heap.Type().Index < limit {
			return true
		}
	}
	// Exact Core 3 declaration and execution profiles contain nullable abstract
	// externref carriers. Their enclosing whole-module proofs establish which
	// functions may execute or publish. Keep these exceptions out of every
	// ordinary control/call directory.
	if (types.declarationExtern || types.executionExtern || types.publicReferenceCarriers) && ref.Nullable() && heap == wasm.AbsHeap(wasm.HeapExtern) {
		return true
	}
	if types.declarationNonNullExtern && !ref.Nullable() && heap == wasm.AbsHeap(wasm.HeapExtern) {
		return true
	}
	if heap == wasm.AbsHeap(wasm.HeapFunc) {
		return true
	}
	return typed && heap.Kind() == wasm.HeapTypeIndex && !heap.Type().Rec && types.function(heap.Type().Index)
}

// Explicit nullable syntax has the same ReferenceTypes semantics as bare
// funcref for the abstract heap. Non-null results and all indexed annotations
// additionally require the private typed profile.
// Both lower through the existing internal full-width result class.
func internalReferenceBlockType(r *wasm.Reader, typed bool, types internalReferenceTypes) (int64, error) {
	lead, ok := r.Peek()
	if ok && (types.gcRefTestAbstract && (lead == 0x6a || lead == 0x6b || lead == 0x6c || lead == 0x6d || lead == 0x6e) || types.gcRefTestConcrete && lead == 0x6b || types.gcBrOnCastNullability && lead == 0x6e) {
		if _, err := r.S33(); err != nil {
			return 0, err
		}
		return -16, nil
	}
	if !ok || lead != 0x63 && lead != 0x64 {
		return r.S33()
	}
	_, err := internalExplicitBlockReferenceType(r, typed, types)
	return -16, err
}

func internalExplicitBlockReferenceType(r *wasm.Reader, typed bool, types internalReferenceTypes) (bool, error) {
	return internalExplicitReferenceTypeMode(r, typed, types, true)
}

// The same explicit annotation parser is used for private carrier admission and
// persisted source requirements. Nullable indexed heaps also need the typed
// witness; nullable abstract func retains ordinary ReferenceTypes semantics.
func internalExplicitReferenceType(r *wasm.Reader, typed bool, types internalReferenceTypes) (bool, error) {
	return internalExplicitReferenceTypeMode(r, typed, types, false)
}

func internalExplicitReferenceTypeMode(r *wasm.Reader, typed bool, types internalReferenceTypes, block bool) (bool, error) {
	lead, err := r.Byte()
	if err != nil {
		return false, err
	}
	if lead != 0x63 && lead != 0x64 {
		return false, unsupported("expected explicit reference type")
	}
	heap, err := r.S33()
	if err != nil {
		return false, err
	}
	requiresTyped := lead == 0x64 || heap >= 0
	if requiresTyped && !typed {
		return false, unsupported("reference annotation requires retained typed-reference protocol")
	}
	declarationExtern := block && types.declarationExtern && lead == 0x64 && heap == -17
	gcAbstract := block && (types.gcRefTestAbstract && (heap == -15 || heap == -18 || heap == -19 || heap == -20 || heap == -21 || heap == -22 || heap == 1 || heap == 2) || types.gcBrOnCastNullability && (heap == -18 || heap == 0))
	runtimeSubtype := types.gcTypeSubtypingRuntimeCallCast && heap >= 0 && heap < 3 && (!types.gcTypeSubtypingFinalityCallCast || heap < 2)
	refTestSubtype := types.gcTypeSubtypingRefTestSingle &&
		((types.gcTypeSubtypingRefTestVariant > 0 && types.gcTypeSubtypingRefTestVariant < 4 && heap == 4) ||
			(types.gcTypeSubtypingRefTestVariant == 4 && heap == 0) ||
			(types.gcTypeSubtypingRefTestVariant == 5 && (heap == 0 || heap == 4)) ||
			(types.gcTypeSubtypingRefTestVariant == 6 && heap >= 0 && heap < 4) ||
			(types.gcTypeSubtypingRefTestVariant == 7 && heap >= 0 && heap < 8) ||
			(types.gcTypeSubtypingRefTestVariant == 8 && heap == 0) ||
			(types.gcTypeSubtypingRefTestVariant == 9 && heap == 2))
	if heap != -16 && !declarationExtern && !gcAbstract && !runtimeSubtype && !refTestSubtype && !(typed && heap >= 0 && (types.function(uint32(heap)) || types.rootedStruct(uint32(heap)) || types.rootedArray(uint32(heap)))) {
		return false, unsupported("explicit reference type requires abstract func or an admitted function-type index")
	}
	return requiresTyped, nil
}

// A select immediate is one value type, not a block type: preserve the caller's
// existing numeric/reference byte allowlist, and canonicalize only explicit
// function references. Void and bare indexed block signatures stay invalid.
func internalSelectType(r *wasm.Reader, typed bool, types internalReferenceTypes) (byte, error) {
	if lead, ok := r.Peek(); ok && (lead == 0x63 || lead == 0x64) {
		_, err := internalExplicitReferenceType(r, typed, types)
		return 0x70, err
	}
	return r.Byte()
}
