//go:build amd64

package amd64

import "math"

func (f *fn) compactScalarArrayLayout(t uint32) (directGCScalar, uint32, bool) {
	if !gcCompactScalarArrays || t > math.MaxInt32/4 {
		return directGCScalar{}, 0, false
	}
	s, final, ok := f.directGCArrayLayout(t)
	if !ok || !final || s.size <= 0 || s.size > 8 {
		return directGCScalar{}, 0, false
	}
	st, ok := f.stagedArraySubtype(t)
	if !ok {
		return directGCScalar{}, 0, false
	}
	align, _, ok := nativeGCStorageLayout(f.m, t, st.Comp.Array.Storage())
	if !ok || align == 0 {
		return directGCScalar{}, 0, false
	}
	return s, (8 + align - 1) &^ (align - 1), true
}

// Full checked compact geometry proves the entire payload inside the active
// object region. Logical bounds plus widened scaling then prove the selected
// scalar span; no synthetic direct-header Size word or trusted lease is used.
func (f *fn) compactScalarArrayAddress(object, indexValue *elem, t uint32, size int, payload uint32, local int) (Reg, Reg, func()) {
	var obj Reg
	var done func()
	leased := f.directLoop.endPC != 0
	if leased {
		obj, done = f.useCompactLoop(object, t, uint32(size), payload, local)
	} else {
		obj, done = f.emitCheckedCompactOffset(object, t, 8, 0, payload, uint32(size))
	}
	index := f.materialize(indexValue)
	f.a.MovRegReg32(index, index)
	f.pinned = f.pinned.add(index)
	tmp := f.allocReg(maskOf(obj, index))
	if leased {
		f.a.MovRegReg32(tmp, f.directLoop.length)
	} else {
		f.a.Load32(tmp, obj, 4)
	}
	f.a.Cmp32(index, tmp)
	f.trapIf(condAE, trapBuiltin)
	f.a.ImulRI(index, int32(size), true)
	return obj, index, func() { f.pinned = f.pinned.remove(index); f.release(index); done() }
}

func (f *fn) emitCompactScalarArrayGet(t, helper uint32) bool {
	s, payload, ok := f.compactScalarArrayLayout(t)
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
	obj, index, done := f.compactScalarArrayAddress(object, indexValue, t, s.size, payload, local)
	if s.typ.isFloat() {
		x := f.allocFReg(0)
		f.a.FLoadIdx(x, obj, index, int32(payload), s.typ == mtF64)
		done()
		f.spillFloor = floor
		f.pushFReg(x, s.typ)
	} else {
		r := f.allocReg(maskOf(obj, index))
		f.a.LoadIdx(r, obj, index, int32(payload), s.size, helper == gcArrayGetS, s.typ == mtI64)
		done()
		f.spillFloor = floor
		f.pushReg(r, s.typ)
	}
	return true
}

func (f *fn) emitCompactScalarArraySet(t uint32) bool {
	s, payload, ok := f.compactScalarArrayLayout(t)
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
	obj, index, done := f.compactScalarArrayAddress(object, indexValue, t, s.size, payload, local)
	if s.typ.isFloat() {
		x := f.materializeF(value)
		f.a.FStoreIdx(obj, index, x, int32(payload), s.typ == mtF64)
		f.releaseF(x)
	} else {
		r := f.materialize(value)
		f.a.StoreIdx(obj, index, r, int32(payload), s.size)
		f.release(r)
	}
	done()
	f.spillFloor = floor
	return true
}
