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
	if types.publicReferenceCarriers && publicReferenceCarrierTarget(signature) {
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
	if ref.Exact() || !ref.Nullable() && !typed {
		return false
	}
	heap := ref.Heap()
	if types.i31 && heap == wasm.AbsHeap(wasm.HeapI31) {
		return true
	}
	if types.gcArrayFixedOfficial || types.gcArrayPackedData || types.gcArrayReference || types.gcArrayInitElem || types.gcArrayInitData || types.gcArrayFill {
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
	if heap != -16 && !declarationExtern && !(typed && heap >= 0 && types.function(uint32(heap))) {
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
