//go:build amd64 && wago_gc_compact_weighted_sum

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type compactWeightedState struct {
	weightedIndex, weightedTotal, weightedBound uint32
	weightedWeight                              int32
}

func (f *fn) prepareCompactWeighted(reader *wasm.Reader, p gcLoopResolverPlan, paired bool) {
	f.weightedBound = 0
	if paired || p.accesses != 1 {
		return
	}
	scalar, _, ok := f.compactScalarArrayLayout(p.typeIndex)
	if !ok || scalar.size != 4 || scalar.typ != mtI32 {
		return
	}
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
	total, ok := local()
	if !ok || int(total) >= len(f.localType) || f.localType[total] != mtI32 {
		return
	}
	index, ok := local()
	if !ok || int(index) >= len(f.localType) || f.localType[index] != mtI32 || index == total || int(index) == p.local || int(total) == p.local {
		return
	}
	if !op(0x41) {
		return
	}
	weight, e := r.I32()
	if e != nil || !op(0x6a) {
		return
	}
	receiver, ok := local()
	if !ok || int(receiver) != p.local {
		return
	}
	ix, ok := local()
	if !ok || ix != index || !op(0xfb) {
		return
	}
	get, e := r.U32()
	if e != nil || get != 11 {
		return
	}
	ty, e := r.U32()
	if e != nil || ty != p.typeIndex || !op(0x6c) || !op(0x6a) || !op(0x21) {
		return
	}
	sum, e := r.U32()
	if e != nil || sum != total {
		return
	}
	ix, ok = local()
	if !ok || ix != index || !op(0x41) {
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
	if e != nil || ix != index {
		return
	}
	if assign == 0x21 {
		ix, ok = local()
		if !ok || ix != index {
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
	f.weightedIndex, f.weightedTotal, f.weightedBound, f.weightedWeight = index, total, uint32(bound), weight
}

func (f *fn) tryCompactWeightedLatch(fr *ctrlFrame, fi, k int, cc Cond) bool {
	l := f.directLoop
	if f.weightedBound == 0 || l.kind != loopLeaseCompactActive || l.remaining != 0 || fi != len(f.ctrl)-1 || fr.kind != cfLoop || fr.paramN != 0 || fr.resultN != 0 || fr.branchArity() != 0 || fr.has(ctrlRegMerge1) || k != 0 || f.depth() != 0 || f.interruptible || f.a.Len()-fr.controlSite > 1024 || cc != condB {
		return false
	}
	index, total := int(f.weightedIndex), int(f.weightedTotal)
	ir, fp, ok := f.pinReg(index)
	if !ok || fp || f.locals[index].state != lsStackReg {
		return false
	}
	tr, fp, ok := f.pinReg(total)
	if !ok || fp || ir == tr || f.locals[total].state != lsStackReg {
		return false
	}
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
	f.a.AluRI(cmpDigit, l.length, int32(f.weightedBound), false)
	short := f.a.JccPlaceholder(condB)
	fast := f.a.Len()
	f.a.MovRegReg32(offset, ir)
	f.a.ImulRI(offset, 4, true)
	f.a.LoadIdx(value, l.object, offset, 8, 4, false, false)
	f.a.LeaDispW(offset, ir, f.weightedWeight, false)
	f.a.IMul(value, offset, false)
	f.a.AluRR(0x01, tr, value, false)
	f.a.AluRI(0, ir, 1, false)
	f.a.AluRI(cmpDigit, ir, int32(f.weightedBound), false)
	again := f.a.JccPlaceholder(condB)
	f.a.PatchRel32(again, fast)
	f.storeLocalReg(index, ir, false)
	f.storeLocalReg(total, tr, false)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(short, f.a.Len())
	f.a.JmpBack(fr.controlSite)
	f.a.PatchRel32(done, f.a.Len())
	f.a.PatchRel32(exit, f.a.Len())
	f.stats.peep("gc-compact-weighted-sum")
	return true
}
