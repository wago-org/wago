//go:build amd64

package amd64

import (
	"math"
)

// Checked numeric memmove. Both null checks and every span check precede writes.
func (f *fn) emitCompactArrayCopy(dstType, srcType uint32) bool {
	if !gcCompactCopy || f.interruptible || dstType > math.MaxInt32/4 || srcType > math.MaxInt32/4 {
		return false
	}
	d, df, dok := f.directGCArrayLayout(dstType)
	s, sf, sok := f.directGCArrayLayout(srcType)
	if !dok || !sok || !df || !sf || d.size != s.size || d.size < 1 || d.size > 8 {
		return false
	}
	// Seven simultaneous general registers are needed after the two resolvers.
	// Under persistent local/global pins, retain the checked helper instead of
	// making an otherwise valid function fail register allocation.
	available := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if !blocked.has(r) {
			available++
		}
	}
	if available < 7 {
		return false
	}
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	countValue, srcIndexValue, source, dstIndexValue, destination := f.popValue(), f.popValue(), f.popValue(), f.popValue(), f.popValue()
	dst, doneDst := f.emitCheckedCompactOffset(destination, dstType, 8, 0, 8, uint32(d.size))
	src, doneSrc := f.emitCheckedCompactOffset(source, srcType, 8, 0, 8, uint32(s.size))
	dstIndex := f.materialize(dstIndexValue)
	f.a.MovRegReg32(dstIndex, dstIndex)
	f.pinned = f.pinned.add(dstIndex)
	srcIndex := f.materialize(srcIndexValue)
	f.a.MovRegReg32(srcIndex, srcIndex)
	f.pinned = f.pinned.add(srcIndex)
	count := f.materialize(countValue)
	f.a.MovRegReg32(count, count)
	f.pinned = f.pinned.add(count)
	tmp := f.allocReg(maskOf(dst, src, dstIndex, srcIndex, count))
	f.pinned = f.pinned.add(tmp)
	end := f.allocReg(maskOf(dst, src, dstIndex, srcIndex, count, tmp))
	f.pinned = f.pinned.add(end)
	// Merge logical failures at one native branch to preserve source-PC fidelity.
	f.a.MovReg64(tmp, dstIndex)
	f.a.Add64(tmp, count)
	f.a.Load32(end, dst, 4)
	f.a.Cmp64(tmp, end)
	badDst := f.a.JccPlaceholder(condA)
	f.a.MovReg64(tmp, srcIndex)
	f.a.Add64(tmp, count)
	f.a.Load32(end, src, 4)
	f.a.Cmp64(tmp, end)
	f.a.PatchRel32(badDst, f.a.Len())
	f.trapIf(condA, trapBuiltin)
	// Both full compact extents were validated before logical spans; no Size word exists.
	f.pinned = f.pinned.remove(tmp).remove(end)
	f.a.ImulRI(dstIndex, int32(d.size), true)
	f.a.Add64(dstIndex, dst)
	f.a.AluRI(0, dstIndex, 8, true)
	f.a.ImulRI(srcIndex, int32(d.size), true)
	f.a.Add64(srcIndex, src)
	f.a.AluRI(0, srcIndex, 8, true)
	f.a.ImulRI(count, int32(d.size), true)
	// Preserve caller register assignments and move fixed string operands without
	// register-cycle hazards. No canonical frame access, call or trap occurs
	// while temporary pushes are active.
	for _, r := range []Reg{RAX, RDI, RSI, RCX} {
		f.a.Push(r)
	}
	f.a.Push(dstIndex)
	f.a.Push(srcIndex)
	f.a.Push(count)
	f.a.Pop(RCX)
	f.a.Pop(RSI)
	f.a.Pop(RDI)
	f.a.TestSelf(RCX, true)
	empty := f.a.JccPlaceholder(condE)
	f.a.Cmp64(RDI, RSI)
	forwardA := f.a.JccPlaceholder(condBE)
	f.a.MovReg64(RAX, RSI)
	f.a.Add64(RAX, RCX)
	f.a.Cmp64(RDI, RAX)
	forwardB := f.a.JccPlaceholder(condAE)
	f.a.Add64(RDI, RCX)
	f.a.AluRI(5, RDI, 1, true)
	f.a.Add64(RSI, RCX)
	f.a.AluRI(5, RSI, 1, true)
	f.a.Std()
	f.a.RepMovsb()
	f.a.Cld()
	copied := f.a.JmpPlaceholder()
	f.a.PatchRel32(forwardA, f.a.Len())
	f.a.PatchRel32(forwardB, f.a.Len())
	f.a.RepMovsb()
	f.a.PatchRel32(empty, f.a.Len())
	f.a.PatchRel32(copied, f.a.Len())
	for _, r := range []Reg{RCX, RSI, RDI, RAX} {
		f.a.Pop(r)
	}
	f.pinned = f.pinned.remove(dstIndex).remove(srcIndex).remove(count)
	f.release(dstIndex)
	f.release(srcIndex)
	f.release(count)
	doneSrc()
	doneDst()
	f.spillFloor = floor
	return true
}
