package codegen

import "github.com/wago-org/wago/src/core/compiler/wasm"

// GCFrameRootLimit is the shared compile/runtime bound on exact mutable roots
// in one native frame. Keeping the bound next to the backend-neutral reference
// classifier lets every backend budget the same metadata product.
const GCFrameRootLimit = 1024

// IsCollectorReferenceType reports whether t is represented by Wago's Wasm GC
// collector. Function, external, and exception references use other runtime
// representations and must not be included in collector root maps. Unresolved
// indexed shapes fail closed by requesting collector handling.
func IsCollectorReferenceType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind() != wasm.ValRef {
		return false
	}
	heap := t.Ref().Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
			return true
		default:
			return false
		}
	case wasm.HeapDefType:
		kind, valid := heap.DefCompKind()
		if !valid {
			return true
		}
		return kind == wasm.CompStruct || kind == wasm.CompArray
	case wasm.HeapTypeIndex:
		if m == nil {
			return true
		}
		index := heap.Type().Index
		for _, group := range m.Types {
			if index < uint32(len(group.SubTypes)) {
				kind := group.SubTypes[index].Comp.Kind
				return kind == wasm.CompStruct || kind == wasm.CompArray
			}
			index -= uint32(len(group.SubTypes))
		}
		return true
	default:
		return true
	}
}
