//go:build amd64

package amd64

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// Numeric fills remain fully checked even when combined with trusted policies.
// Every trap precedes the first store, including zero-length span validation.
func (f *fn) emitDirectOffsetArrayFill(t uint32) bool {
	if !gcDirectFill || f.interruptible || t > math.MaxInt32/4 {
		return false
	}
	s, final, ok := f.directGCArrayLayout(t)
	if !ok || !final || s.size < 1 || s.size > 8 {
		return false
	}
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	countValue, value, indexValue, object := f.popValue(), f.popValue(), f.popValue(), f.popValue()
	obj, done := f.emitFullyCheckedDirectOffset(object, t, gc.PayloadOffset)
	index := f.materialize(indexValue)
	f.a.MovRegReg32(index, index)
	f.pinned = f.pinned.add(index)
	count := f.materialize(countValue)
	f.a.MovRegReg32(count, count)
	f.pinned = f.pinned.add(count)
	tmp := f.allocReg(maskOf(obj, index, count))
	f.pinned = f.pinned.add(tmp)
	f.a.Load32(tmp, obj, 8)
	f.a.Cmp32(index, tmp)
	f.trapIf(condA, trapBuiltin)
	f.a.Sub32(tmp, index)
	f.a.Cmp32(count, tmp)
	f.trapIf(condA, trapBuiltin)
	f.a.MovReg64(tmp, index)
	f.a.Add64(tmp, count)
	f.a.ImulRI(tmp, int32(s.size), true)
	f.a.AluRI(0, tmp, int32(gc.PayloadOffset), true)
	end := f.allocReg(maskOf(obj, index, count, tmp))
	f.pinned = f.pinned.add(end)
	f.a.Load32(end, obj, 4)
	f.a.Cmp64(tmp, end)
	f.trapIf(condA, trapCastFailure)
	f.pinned = f.pinned.remove(end)
	f.pinned = f.pinned.remove(tmp)
	f.a.ImulRI(index, int32(s.size), true)
	// Materialize once before the loop so even NaN payload bits remain unchanged.
	var vr Reg
	if s.typ.isFloat() {
		vr = f.materializeF(value)
	} else {
		vr = f.materialize(value)
		f.pinned = f.pinned.add(vr)
	}
	f.a.TestSelf(count, false)
	empty := f.a.JccPlaceholder(condE)
	loop := f.a.Len()
	if s.typ.isFloat() {
		f.a.FStoreIdx(obj, index, vr, int32(gc.PayloadOffset), s.typ == mtF64)
	} else {
		f.a.StoreIdx(obj, index, vr, int32(gc.PayloadOffset), s.size)
	}
	f.a.AluRI(0, index, int32(s.size), true)
	f.a.AluRI(5, count, 1, false)
	again := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(again, loop)
	f.a.PatchRel32(empty, f.a.Len())
	if s.typ.isFloat() {
		f.releaseF(vr)
	} else {
		f.pinned = f.pinned.remove(vr)
		f.release(vr)
	}
	f.pinned = f.pinned.remove(index).remove(count)
	f.release(index)
	f.release(count)
	done()
	f.spillFloor = floor
	return true
}
