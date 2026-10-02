package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
)

// MaxEHTagPayloadWords bounds the parameters of one exception tag.
const MaxEHTagPayloadWords = 8

// EHPayloadRootKind classifies one exception payload word the way native frame
// roots classify locals: funcref-family words keep a funcref identity,
// collector references are scanned as gc.Ref, and scalars and extern/exn
// references (which the collector does not own) are unrooted.
func EHPayloadRootKind(m *wasm.Module, typ wasm.ValType) (nativeabi.RootKind, bool) {
	if typ.Kind() != wasm.ValRef {
		return 0, false
	}
	heap := typ.Ref().Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapFunc, wasm.HeapNoFunc:
			return nativeabi.RootFuncRef, true
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
			return nativeabi.RootGCRef, true
		default:
			return 0, false
		}
	case wasm.HeapTypeIndex:
		var ft wasm.CompType
		if m.ResolveTypeFunc(heap.Type().Index, &ft) {
			return nativeabi.RootFuncRef, true
		}
		return nativeabi.RootGCRef, true
	case wasm.HeapDefType:
		if kind, valid := heap.DefCompKind(); valid && kind == wasm.CompFunc {
			return nativeabi.RootFuncRef, true
		}
		return nativeabi.RootGCRef, true
	default:
		return 0, false
	}
}

// Exception payloads use one fixed layout on every backend and in every module,
// because a throw writes straight into the handler's record, which may belong
// to a foreign instance. Parameter i of a tag lives in lane i, except that a
// collector reference lives in lane EHGCLaneBase+i instead. GC lanes therefore
// hold only gc.Ref values or zero, so a catch_all_ref slot can be scanned
// exactly whatever tag it caught; throws zero the GC lanes they do not write.
const (
	EHGCLaneBase   = MaxEHTagPayloadWords
	EHPayloadLanes = 2 * MaxEHTagPayloadWords
)

// EHPayloadLane returns the lane of a tag parameter of type typ at index i.
func EHPayloadLane(m *wasm.Module, typ wasm.ValType, i int) int {
	if kind, ok := EHPayloadRootKind(m, typ); ok && kind == nativeabi.RootGCRef {
		return EHGCLaneBase + i
	}
	return i
}

// TagType resolves tag index (imports first) to its declared tag type.
func TagType(m *wasm.Module, index uint32) (wasm.TagType, bool) {
	for i := range m.Imports {
		im := &m.Imports[i]
		if im.Type.Kind != wasm.ExternTag {
			continue
		}
		if index == 0 {
			return im.Type.TagType(), true
		}
		index--
	}
	if int(index) >= len(m.Tags) {
		return wasm.TagType{}, false
	}
	return m.Tags[index], true
}
