//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// tryForwardGCArrayConstructorGet recognizes an immediately consumed uniform
// numeric array. Only effect-free constant or masked-local indexes are skipped;
// their unsigned upper bound must be below the known constructor length. The
// allocation remains observable, and only re-readable initializer values cross
// its helper/safepoint.
func (f *fn) tryForwardGCArrayConstructorGet(r *wasm.Reader, typeIndex uint32, field wasm.FieldType) (storage, wasm.Reader, bool) {
	fail := func() (storage, wasm.Reader, bool) { return storage{}, wasm.Reader{}, false }
	if field.Storage().Packed() || field.Storage().Val().Kind() != wasm.ValNum {
		return fail()
	}
	length := f.s.back()
	if length == nil || length.elemKind() != ekValue || length.st.kind != stConst || length.st.typ != mtI32 {
		return fail()
	}
	n := uint32(length.st.cval)
	value := f.s.prev(length)
	if value == nil || value.elemKind() != ekValue {
		return fail()
	}
	forward := value.st
	switch forward.kind {
	case stConst:
	case stLocalRef, stLocalReg:
		forward.kind = stLocalRef
		forward.reg = regNone
	default:
		return fail()
	}
	next, ok := matchGCBoundedArrayGet(r, typeIndex, n)
	if !ok {
		return fail()
	}
	return forward, next, true
}

func matchGCBoundedArrayGet(r *wasm.Reader, typeIndex, count uint32) (wasm.Reader, bool) {
	fail := func() (wasm.Reader, bool) { return wasm.Reader{}, false }
	next := *r
	op, err := next.Byte()
	if err != nil {
		return fail()
	}
	masked := op == 0x20
	if masked {
		if _, err = next.U32(); err != nil {
			return fail()
		}
		op, err = next.Byte()
		if err != nil {
			return fail()
		}
	}
	if op != 0x41 {
		return fail()
	}
	bound, err := next.I32()
	if err != nil || uint32(bound) >= count {
		return fail()
	}
	if masked {
		if op, err = next.Byte(); err != nil || op != 0x71 {
			return fail()
		}
	}
	if op, err = next.Byte(); err != nil || op != 0xfb {
		return fail()
	}
	sub, err := next.U32()
	if err != nil || sub != 11 {
		return fail()
	}
	access, err := next.U32()
	if err != nil || access != typeIndex {
		return fail()
	}
	return next, true
}

func (f *fn) forwardArrayChargeSize(typeIndex uint32) uint32 {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray)
	if !ok || !layout.Type.Final || !layout.PointerFree || !layout.ElemLayout.DirectAccess {
		return 0
	}
	size := layout.ElemLayout.Size
	if size != 4 && size != 8 {
		return 0
	}
	// The forwarding proof has already established a constant, positive length.
	length := uint32(f.s.back().st.cval)
	if length > 64 {
		return 0
	}
	return gc.Align8(gc.PayloadOffset + length*size)
}
