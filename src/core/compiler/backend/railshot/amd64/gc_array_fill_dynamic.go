//go:build amd64

package amd64

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// emitDynamicGCArrayFill checks the complete logical and physical range before
// writing numeric elements. The kernel uses bounded code for all lengths.
func (f *fn) emitDynamicGCArrayFill(typeIndex uint32) bool {
	scalar, final, ok := f.directGCArrayLayout(typeIndex)
	if !gcSharedStubsEnabled || !ok || !final {
		return false
	}
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	count := f.popValue()
	value := f.popValue()
	index := f.popValue()
	object := f.popValue()
	countSlot, valueSlot, indexSlot := count.st.slotIndex(), value.st.slotIndex(), index.st.slotIndex()
	obj := f.emitSharedCheckedGCObjectPinned(object, typeIndex, gc.PayloadOffset, gcSharedStubResolveBoundedObject)
	f.stats.addGCHandleResolution()
	f.a.MovReg64(R8, obj)
	f.pinned = f.pinned.remove(obj)
	vec := f.allocFReg(0) // any spill must occur before the zero-count branch
	f.a.Load32(RCX, RSP, f.spillOff(countSlot))
	f.a.Load32(RDI, RSP, f.spillOff(indexSlot))
	// The sum of two zero-extended i32 values fits in 64 bits.
	f.a.MovReg64(RDX, RDI)
	f.a.Add64(RDX, RCX)
	f.a.Load32(RAX, R8, 8)
	f.a.Cmp64(RDX, RAX)
	f.trapIf(condA, trapBuiltin)
	f.a.TestSelf(RCX, false)
	zero := f.a.JccPlaceholder(condE)
	f.a.ImulRI(RDX, int32(scalar.size), true)
	f.a.AluRI(0, RDX, int32(gc.PayloadOffset), true)
	f.a.Load32(RAX, R8, 4)
	f.a.Cmp64(RDX, RAX)
	f.trapIf(condA, trapCastFailure)
	f.a.ImulRI(RDI, int32(scalar.size), true)
	f.a.Add64(RDI, R8)
	f.a.AluRI(0, RDI, int32(gc.PayloadOffset), true)
	f.a.ImulRI(RCX, int32(scalar.size), true)
	// Read raw scalar bits, including floating-point NaNs, from canonical slots.
	f.a.Load64(RAX, RSP, f.spillOff(valueSlot))
	switch scalar.size {
	case 1:
		f.a.AluRI(4, RAX, 0xff, false)
		f.a.MovImm64(RDX, 0x0101010101010101)
		f.a.IMul(RAX, RDX, true)
	case 2:
		f.a.AluRI(4, RAX, 0xffff, false)
		f.a.MovImm64(RDX, 0x0001000100010001)
		f.a.IMul(RAX, RDX, true)
	case 4:
		f.a.MovRegReg32(RAX, RAX)
		f.a.MovReg64(RDX, RAX)
		f.a.ShiftImm(4, RDX, 32, true)
		f.a.AluRR(0x09, RAX, RDX, true)
	}
	// Large fills use the fast string engine. Preserve the subword tail.
	f.a.AluRI(cmpDigit, RCX, 512, true)
	vector := f.a.JccPlaceholder(condB)
	f.a.MovReg64(RDX, RCX)
	f.a.AluRI(4, RDX, 7, true)
	f.a.ShiftImm(5, RCX, 3, true)
	f.a.RepStosq()
	f.a.MovReg64(RCX, RDX)
	stringTail := f.a.JmpPlaceholder()
	f.a.PatchRel32(vector, f.a.Len())
	f.a.MovGprToXmm(vec, RAX, true)
	f.a.Punpcklqdq(vec, vec)
	f.a.AluRI(cmpDigit, RCX, 64, true)
	short := f.a.JccPlaceholder(condB)
	loop64 := f.a.Len()
	for _, offset := range []int32{-16, -32, -48, -64} {
		f.mov128StoreIdx(RDI, RCX, vec, offset)
	}
	f.a.AluRI(5, RCX, 64, true)
	f.a.AluRI(cmpDigit, RCX, 64, true)
	f.a.PatchRel32(f.a.JccPlaceholder(condAE), loop64)
	f.a.PatchRel32(short, f.a.Len())
	f.a.AluRI(cmpDigit, RCX, 16, true)
	tail := f.a.JccPlaceholder(condB)
	loop16 := f.a.Len()
	f.mov128StoreIdx(RDI, RCX, vec, -16)
	f.a.AluRI(5, RCX, 16, true)
	f.a.AluRI(cmpDigit, RCX, 16, true)
	f.a.PatchRel32(f.a.JccPlaceholder(condAE), loop16)
	f.a.PatchRel32(tail, f.a.Len())
	f.a.PatchRel32(stringTail, f.a.Len())
	f.a.TestSelf(RCX, true)
	done := f.a.JccPlaceholder(condE)
	loopScalar := f.a.Len()
	f.a.StoreIdx(RDI, RCX, RAX, -int32(scalar.size), scalar.size)
	f.a.AluRI(5, RCX, int32(scalar.size), true)
	f.a.PatchRel32(f.a.JccPlaceholder(condNE), loopScalar)
	f.a.PatchRel32(done, f.a.Len())
	f.a.PatchRel32(zero, f.a.Len())
	f.releaseF(vec)
	f.releaseDroppedValue(count)
	f.releaseDroppedValue(value)
	f.releaseDroppedValue(index)
	f.spillFloor = oldFloor
	f.stats.peep("gc-array-native-fill-dynamic")
	return true
}
