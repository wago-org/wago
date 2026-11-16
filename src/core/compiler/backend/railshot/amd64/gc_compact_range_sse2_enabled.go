//go:build amd64 && (wago_gc_compact_range_sse2 || (wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2))

package amd64

const gcCompactRangeSSE2 = true

// Called only after the original first checked iteration and both full-span
// guards. Failure falls through to the existing ordered scalar loop untouched.
func (f *fn) emitCompactRangeSSE2(l directLoopLease, index, offset, value Reg, width int, payload uint32) int {
	lanes := uint32(16 / width)
	if f.rangeBound < lanes {
		return -1
	}
	x := f.freeFReg(0)
	if x == regNone {
		return -1
	}
	f.fpinned = f.fpinned.add(x)
	f.a.AluRI(cmpDigit, index, int32(f.rangeBound-lanes), false)
	short := f.a.JccPlaceholder(condA)
	// Same index/layout makes equal bases an identical touched span. Otherwise
	// compare absolute base displacement against the widened remaining byte count.
	f.a.Cmp64(l.object, l.length)
	same := f.a.JccPlaceholder(condE)
	f.a.MovRegReg32(offset, index)
	f.a.MovImm64(value, uint64(f.rangeBound))
	f.a.AluRR(0x29, value, offset, true)
	f.a.ImulRI(value, int32(width), true)
	f.a.Cmp64(l.object, l.length)
	sourceAbove := f.a.JccPlaceholder(condAE)
	f.a.MovReg64(offset, l.length)
	f.a.AluRR(0x29, offset, l.object, true)
	deltaReady := f.a.JmpPlaceholder()
	f.a.PatchRel32(sourceAbove, f.a.Len())
	f.a.MovReg64(offset, l.object)
	f.a.AluRR(0x29, offset, l.length, true)
	f.a.PatchRel32(deltaReady, f.a.Len())
	f.a.Cmp64(offset, value)
	dependent := f.a.JccPlaceholder(condB)
	f.a.PatchRel32(same, f.a.Len())
	f.a.MovRegReg32(offset, index)
	f.a.ImulRI(offset, int32(width), true)
	vector := f.a.Len()
	f.a.MovdquLoadIdx(x, l.object, offset, int32(payload))
	f.a.MovdquStoreIdx(l.length, offset, x, int32(payload))
	f.a.AluRI(0, offset, 16, true)
	f.a.AluRI(0, index, int32(lanes), false)
	f.a.AluRI(cmpDigit, index, int32(f.rangeBound-lanes), false)
	again := f.a.JccPlaceholder(condBE)
	f.a.PatchRel32(again, vector)
	f.a.AluRI(cmpDigit, index, int32(f.rangeBound), false)
	done := f.a.JccPlaceholder(condE)
	f.a.PatchRel32(short, f.a.Len())
	f.a.PatchRel32(dependent, f.a.Len())
	f.fpinned = f.fpinned.remove(x)
	f.releaseF(x)
	f.stats.peep("gc-compact-range-sse2")
	return done
}
