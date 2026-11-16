//go:build amd64 && (wago_gc_compact_range_loop || (wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum) || wago_gc_compact_range_stride)

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Present only in this experimental compiler. No runtime or default fn growth.
type compactRangeState struct{ rangeIndex, rangeBound uint32 }

func (f *fn) prepareCompactRange(reader *wasm.Reader, p gcLoopResolverPlan) {
	f.rangeBound = 0
	r := *reader
	b, e := r.Bytes(min(128, r.BytesLeft()))
	if e != nil {
		return
	}
	r = wasm.ReaderFrom(b)
	op := func(w byte) bool { v, e := r.Byte(); return e == nil && v == w }
	local := func() (uint32, bool) {
		if !op(0x20) {
			return 0, false
		}
		v, e := r.U32()
		return v, e == nil
	}
	dst, ok := local()
	if !ok || dst != uint32(p.secondLocal) {
		return
	}
	idx, ok := local()
	if !ok || int(idx) >= len(f.localType) || f.localType[idx] != mtI32 {
		return
	}
	src, ok := local()
	if !ok || src != uint32(p.local) {
		return
	}
	ix, ok := local()
	if !ok || ix != idx || !op(0xfb) {
		return
	}
	get, e := r.U32()
	if e != nil || get < 11 || get > 13 {
		return
	}
	ty, e := r.U32()
	if e != nil || ty != p.typeIndex || !op(0xfb) {
		return
	}
	set, e := r.U32()
	if e != nil || set != 14 {
		return
	}
	ty, e = r.U32()
	if e != nil || ty != p.typeIndex {
		return
	}
	ix, ok = local()
	if !ok || ix != idx || !op(0x41) {
		return
	}
	one, e := r.I32()
	if e != nil || one != 1 || !op(0x6a) {
		return
	}
	assign, e := r.Byte()
	if e != nil || (assign != 0x21 && assign != 0x22) {
		return
	}
	ix, e = r.U32()
	if e != nil || ix != idx {
		return
	}
	if assign == 0x21 {
		ix, ok = local()
		if !ok || ix != idx {
			return
		}
	}
	if !op(0x41) {
		return
	}
	bound, e := r.I32()
	if e != nil || bound <= 0 || !op(0x49) || !op(0x0d) {
		return
	}
	label, e := r.U32()
	if e != nil || label != 0 || !op(0x0b) {
		return
	}
	if uint64(f.tracePCBase)+uint64(reader.Offset())+uint64(r.Offset())-1 != uint64(p.endPC) {
		return
	}
	f.rangeIndex, f.rangeBound = idx, uint32(bound)
}

// The original checked body serves initial and failing-span iterations. Only a
// completed iteration can reach this latch, so both pointers are validated.
func (f *fn) tryCompactRangeLatch(fr *ctrlFrame, fi, k int, cc Cond) bool {
	l := f.directLoop
	if f.rangeBound == 0 || l.kind != loopLeaseCompactPair || l.remaining != 0 || fi != len(f.ctrl)-1 || fr.kind != cfLoop || fr.paramN != 0 || fr.resultN != 0 || fr.branchArity() != 0 || fr.has(ctrlRegMerge1) || k != 0 || f.depth() != 0 || f.interruptible || f.a.Len()-fr.controlSite > 1024 || cc != condB {
		return false
	}
	idx := int(f.rangeIndex)
	ir, fp, ok := f.pinReg(idx)
	if !ok || fp || f.locals[idx].state != lsStackReg {
		return false
	}
	scalar, payload, ok := f.compactScalarArrayLayout(l.typeIndex)
	if !ok || scalar.typ.isFloat() {
		return false
	}
	// Clean scratch selection emits no spills or flag-changing instructions before
	// the original predicate branch. Neither receiver nor any local is clobbered.
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	offset, value := regNone, regNone
	for _, r := range gpAlloc {
		if f.regUser[r] == nil && !blocked.has(r) {
			if offset == regNone {
				offset = r
			} else {
				value = r
				break
			}
		}
	}
	if offset == regNone || value == regNone {
		return false
	}
	exit := f.a.JccPlaceholder(invertCond(cc))
	f.a.Load32(value, l.object, 4)
	f.a.AluRI(cmpDigit, value, int32(f.rangeBound), false)
	badA := f.a.JccPlaceholder(condB)
	f.a.Load32(value, l.length, 4)
	f.a.AluRI(cmpDigit, value, int32(f.rangeBound), false)
	badB := f.a.JccPlaceholder(condB)
	vectorDone := f.emitCompactRangeSSE2(l, ir, offset, value, scalar.size, payload)
	// Only a positive sign-extended immediate can compare the widened byte
	// endpoint. Other bounds retain the existing element-index loop.
	byteEnd := uint64(f.rangeBound) * uint64(scalar.size)
	stride := gcCompactRangeStride && byteEnd <= 0x7fffffff
	if stride {
		f.a.MovRegReg32(offset, ir)
		f.a.ImulRI(offset, int32(scalar.size), true)
	}
	fast := f.a.Len()
	if !stride {
		f.a.MovRegReg32(offset, ir)
		f.a.ImulRI(offset, int32(scalar.size), true)
	}
	f.a.LoadIdx(value, l.object, offset, int32(payload), scalar.size, false, scalar.typ == mtI64)
	f.a.StoreIdx(l.length, offset, value, int32(payload), scalar.size)
	if stride {
		f.a.AluRI(0, offset, int32(scalar.size), true)
		f.a.AluRI(cmpDigit, offset, int32(byteEnd), true)
	} else {
		f.a.AluRI(0, ir, 1, false)
		f.a.AluRI(cmpDigit, ir, int32(f.rangeBound), false)
	}
	again := f.a.JccPlaceholder(condB)
	f.a.PatchRel32(again, fast)
	if stride {
		f.a.MovImm32(ir, int32(f.rangeBound))
		f.stats.peep("gc-compact-range-stride")
	}
	if vectorDone >= 0 {
		f.a.PatchRel32(vectorDone, f.a.Len())
	}
	// The index is the only modified local. Preserve the same clean pin+slot
	// contract as the original converged latch, including the false first edge.
	f.storeLocalReg(idx, ir, false)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(badA, f.a.Len())
	f.a.PatchRel32(badB, f.a.Len())
	f.a.JmpBack(fr.controlSite)
	f.a.PatchRel32(done, f.a.Len())
	f.a.PatchRel32(exit, f.a.Len())
	f.stats.peep("gc-compact-range-loop")
	return true
}
