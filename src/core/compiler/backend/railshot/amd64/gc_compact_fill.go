//go:build amd64

package amd64

import (
	"math"
)

// Numeric fills remain fully checked even when combined with trusted policies.
// Every trap precedes the first store, including zero-length span validation.
func (f *fn) emitCompactArrayFill(t uint32) bool {
	if !gcCompactFill || f.interruptible || t > math.MaxInt32/4 {
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
	obj, done := f.emitCheckedCompactOffset(object, t, 8, 0, 8, uint32(s.size))
	index := f.materialize(indexValue)
	f.a.MovRegReg32(index, index)
	f.pinned = f.pinned.add(index)
	count := f.materialize(countValue)
	f.a.MovRegReg32(count, count)
	f.pinned = f.pinned.add(count)
	tmp := f.allocReg(maskOf(obj, index, count))
	f.pinned = f.pinned.add(tmp)
	// One widened sum cannot overflow: both operands were zero-extended i32.
	// A single logical trap site also preserves this opcode's precise PC.
	f.a.MovReg64(tmp, index)
	f.a.Add64(tmp, count)
	end := f.allocReg(maskOf(obj, index, count, tmp))
	f.pinned = f.pinned.add(end)
	f.a.Load32(end, obj, 4)
	f.a.Cmp64(tmp, end)
	f.trapIf(condA, trapBuiltin)
	// Whole compact stride was verified; the widened logical span proves payload bounds.
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
	vectorDone := -1
	{
		avoid := regMask(0)
		if s.typ.isFloat() {
			avoid = maskOf(vr)
		}
		if x := f.freeFReg(avoid); x != regNone {
			f.stats.peep("gc-compact-fill-sse2")
			f.fpinned = f.fpinned.add(x)
			lanes := int32(16 / s.size)
			f.a.AluRI(cmpDigit, count, lanes, false)
			short := f.a.JccPlaceholder(condB)
			if s.typ.isFloat() {
				f.a.FMov(x, vr, s.size == 8)
			} else {
				f.a.MovGprToXmm(x, vr, s.size == 8)
			}
			switch s.size {
			case 1:
				f.a.SseRR(0x66, 0x60, x, x, false) // punpcklbw
				f.a.SseRR(0x66, 0x61, x, x, false) // punpcklwd
				f.a.Pshufd(x, x, 0)
			case 2:
				f.a.SseRR(0x66, 0x61, x, x, false)
				f.a.Pshufd(x, x, 0)
			case 4:
				f.a.Pshufd(x, x, 0)
			case 8:
				f.a.Punpcklqdq(x, x)
			}
			vectorLoop := f.a.Len()
			f.a.MovdquStoreIdx(obj, index, x, 8)
			f.a.AluRI(0, index, 16, true)
			f.a.AluRI(5, count, lanes, false)
			f.a.AluRI(cmpDigit, count, lanes, false)
			again := f.a.JccPlaceholder(condAE)
			f.a.PatchRel32(again, vectorLoop)
			f.a.TestSelf(count, false)
			vectorDone = f.a.JccPlaceholder(condE)
			f.a.PatchRel32(short, f.a.Len())
			f.fpinned = f.fpinned.remove(x)
			f.releaseF(x)
		}
	}
	loop := f.a.Len()
	if s.typ.isFloat() {
		f.a.FStoreIdx(obj, index, vr, 8, s.typ == mtF64)
	} else {
		f.a.StoreIdx(obj, index, vr, 8, s.size)
	}
	f.a.AluRI(0, index, int32(s.size), true)
	f.a.AluRI(5, count, 1, false)
	again := f.a.JccPlaceholder(condNE)
	f.a.PatchRel32(again, loop)
	f.a.PatchRel32(empty, f.a.Len())
	if vectorDone >= 0 {
		f.a.PatchRel32(vectorDone, f.a.Len())
	}
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
