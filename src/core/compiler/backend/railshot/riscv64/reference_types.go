//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Internal function references share one full-width carrier. The frontend proves
// nullability, initialization and subtyping before lowering; this helper admits
// only the abstract function heap, never indexed, exact or non-function types.
func internalFunctionReference(typ wasm.ValType, nonNull bool) bool {
	if typ.Kind() != wasm.ValRef {
		return false
	}
	ref := typ.Ref()
	return !ref.Exact() && (ref.Nullable() || nonNull) && ref.Heap() == wasm.AbsHeap(wasm.HeapFunc)
}

// Explicit nullable syntax has the same ReferenceTypes semantics as bare
// funcref. Non-null results additionally require the private typed profile.
// Both lower through the existing internal full-width result class.
func internalReferenceBlockType(r *wasm.Reader, nonNull bool) (int64, error) {
	lead, ok := r.Peek()
	if !ok || lead != 0x63 && lead != 0x64 {
		return r.S33()
	}
	if lead == 0x64 && !nonNull {
		return 0, unsupported("non-null block requires private typed references")
	}
	if _, err := r.Byte(); err != nil {
		return 0, err
	}
	heap, err := r.S33()
	if err != nil {
		return 0, err
	}
	if heap != -16 {
		return 0, unsupported("explicit reference block requires abstract function heap")
	}
	return -16, nil
}

// A select immediate is one value type, not a block type: preserve the caller's
// existing numeric/reference byte allowlist, and canonicalize only explicit
// abstract function references. Void and indexed block signatures stay invalid.
func internalSelectType(r *wasm.Reader, nonNull bool) (byte, error) {
	if lead, ok := r.Peek(); ok && (lead == 0x63 || lead == 0x64) {
		_, err := internalReferenceBlockType(r, nonNull)
		return 0x70, err
	}
	return r.Byte()
}
