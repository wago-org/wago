//go:build amd64

package amd64

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// Scalar arrays remain fully checked unless their separate policy3 attestation
// is explicitly selected. Trusted scalar-struct reads alone do not authorize it.
func (f *fn) directOffsetScalarArrayLayout(t uint32) (directGCScalar, bool) {
	if !gcDirectScalarArrays {
		return directGCScalar{}, false
	}
	s, final, ok := f.directGCArrayLayout(t)
	return s, ok && final && s.size > 0 && s.size <= 8 && t <= math.MaxInt32/4
}

// Resolving the object precedes logical bounds; scaling uses a zero-extended
// Wasm index and 64-bit arithmetic. Checked mode additionally guards the selected
// physical extent. Policy3 relies on allocator-certified immutable Aux/layout
// instead; it never removes the guest-visible logical bounds check.
func (f *fn) directOffsetScalarArrayAddress(object, indexValue *elem, t uint32, size int, local int) (Reg, Reg, func()) {
	var obj Reg
	var done func()
	leased := f.directLoop.endPC != 0
	if leased {
		if f.directLoop.remaining == 0 || f.wasmPC >= f.directLoop.endPC {
			panic("amd64: unproved direct loop access")
		}
		f.directLoop.remaining--
		// The bounded bytecode proof covers every array access in the lease.
		// Canonical spills may erase machine-value provenance; an unknown
		// provenance still refers to that proved receiver. A known mismatch
		// is a compiler invariant failure, never permission to replace it.
		if (local >= 0 && uint32(local) != f.directLoop.local) || t != f.directLoop.typeIndex {
			panic("amd64: replacing direct loop receiver")
		}
		ref := f.materialize(object)
		f.release(ref)
		obj, done = f.directLoop.object, func() {}
		f.a.TestSelf(obj, true)
		f.trapIf(condE, trapNullReference)
		f.stats.peep("gc-direct-loop-reuse")
	} else if gcTrustedDirectArrays {
		obj, done = f.emitTrustedDirectOffset(object)
	} else {
		obj, done = f.emitFullyCheckedDirectOffset(object, t, gc.PayloadOffset)
	}
	index := f.materialize(indexValue)
	f.a.MovRegReg32(index, index)
	f.pinned = f.pinned.add(index)
	tmp := f.allocReg(maskOf(obj, index))
	f.pinned = f.pinned.add(tmp)
	if leased {
		f.a.MovRegReg32(tmp, f.directLoop.length)
	} else {
		f.a.Load32(tmp, obj, 8)
	}
	f.a.Cmp32(index, tmp)
	f.trapIf(condAE, trapBuiltin)
	f.a.ImulRI(index, int32(size), true)
	if !gcTrustedDirectArrays {
		f.a.Load32(tmp, obj, 4)
		end := f.allocReg(maskOf(obj, index, tmp))
		f.pinned = f.pinned.add(end)
		f.a.MovReg64(end, index)
		f.a.AluRI(0, end, int32(gc.PayloadOffset)+int32(size), true)
		f.a.Cmp64(end, tmp)
		f.trapIf(condA, trapCastFailure)
		f.pinned = f.pinned.remove(end)
	}
	f.pinned = f.pinned.remove(tmp)
	return obj, index, func() {
		f.pinned = f.pinned.remove(index)
		f.release(index)
		done()
	}
}

func (f *fn) emitDirectOffsetScalarArrayGet(t, helper uint32) bool {
	s, ok := f.directOffsetScalarArrayLayout(t)
	if !ok {
		return false
	}
	local := -1
	if x, ok := gcLocalProvenance(f.s.back().prev); ok {
		local = x
	}
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	indexValue, object := f.popValue(), f.popValue()
	obj, index, done := f.directOffsetScalarArrayAddress(object, indexValue, t, s.size, local)
	if s.typ.isFloat() {
		x := f.allocFReg(0)
		f.a.FLoadIdx(x, obj, index, int32(gc.PayloadOffset), s.typ == mtF64)
		done()
		f.spillFloor = floor
		f.pushFReg(x, s.typ)
	} else {
		r := f.allocReg(maskOf(obj, index))
		f.a.LoadIdx(r, obj, index, int32(gc.PayloadOffset), s.size, helper == gcArrayGetS, s.typ == mtI64)
		done()
		f.spillFloor = floor
		f.pushReg(r, s.typ)
	}
	return true
}

func (f *fn) emitDirectOffsetScalarArraySet(t uint32) bool {
	s, ok := f.directOffsetScalarArrayLayout(t)
	if !ok {
		return false
	}
	local := -1
	if x, ok := gcLocalProvenance(f.s.back().prev.prev); ok {
		local = x
	}
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	value, indexValue, object := f.popValue(), f.popValue(), f.popValue()
	obj, index, done := f.directOffsetScalarArrayAddress(object, indexValue, t, s.size, local)
	if s.typ.isFloat() {
		x := f.materializeF(value)
		f.a.FStoreIdx(obj, index, x, int32(gc.PayloadOffset), s.typ == mtF64)
		f.releaseF(x)
	} else {
		r := f.materialize(value)
		f.a.StoreIdx(obj, index, r, int32(gc.PayloadOffset), s.size)
		f.release(r)
	}
	done()
	f.spillFloor = floor
	return true
}
