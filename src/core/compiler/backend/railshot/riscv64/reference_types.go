//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A borrowed directory reuses the signatures admitted by the module profile.
// Exactly one view is used; resolving an annotation never allocates or converts
// the scalar width directory into a second type representation.
type internalReferenceTypes struct {
	scalar []functionSpec
	swar   []*wasm.CompType
}

func (types internalReferenceTypes) function(index uint32) bool {
	var signature *wasm.CompType
	limit := 256
	if types.scalar != nil {
		if types.swar != nil || len(types.scalar) > MaxFunctions || uint64(index) >= uint64(len(types.scalar)) {
			return false
		}
		signature = types.scalar[index].signature
	} else {
		if len(types.swar) > MaxFunctions || uint64(index) >= uint64(len(types.swar)) {
			return false
		}
		signature, limit = types.swar[index], maxSWARLocals
	}
	if signature == nil || signature.Kind != wasm.CompFunc || len(signature.Params) > limit || len(signature.Results) > limit {
		return false
	}
	for _, list := range [][]wasm.ValType{signature.Params, signature.Results} {
		for _, typ := range list {
			if types.scalar != nil && !scalarType(typ, true) || types.scalar == nil && !swarCarrierType(typ) {
				return false
			}
		}
	}
	return true
}

// Production keeps indexed annotations closed until their source/artifact
// witness is separately qualified. This wrapper admits only abstract func.
func internalFunctionReference(typ wasm.ValType, nonNull bool) bool {
	return internalFunctionReferenceWithTypes(typ, nonNull, internalReferenceTypes{})
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
	if lead == 0x64 && !typed {
		return 0, unsupported("non-null block requires private typed references")
	}
	if _, err := r.Byte(); err != nil {
		return 0, err
	}
	heap, err := r.S33()
	if err != nil {
		return 0, err
	}
	if heap != -16 && !(typed && heap >= 0 && types.function(uint32(heap))) {
		return 0, unsupported("explicit reference block requires abstract func or an admitted function-type index")
	}
	return -16, nil
}

// A select immediate is one value type, not a block type: preserve the caller's
// existing numeric/reference byte allowlist, and canonicalize only explicit
// function references. Void and bare indexed block signatures stay invalid.
func internalSelectType(r *wasm.Reader, typed bool, types internalReferenceTypes) (byte, error) {
	if lead, ok := r.Peek(); ok && (lead == 0x63 || lead == 0x64) {
		_, err := internalReferenceBlockType(r, typed, types)
		return 0x70, err
	}
	return r.Byte()
}
