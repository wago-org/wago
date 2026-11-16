//go:build amd64

package amd64

import (
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// Bounded runtime initialization avoids code-size growth with array length.
func (f *fn) compactArrayAllocLayout(t uint32) (directGCScalar, bool) {
	if !gcCompactArrayAlloc || f.interruptible || t > math.MaxInt32/4 {
		return directGCScalar{}, false
	}
	s, final, ok := f.directGCArrayLayout(t)
	return s, ok && final && s.size > 0 && s.size <= 8
}

// Returns flags with ZF clear on success. Original canonical initializer slots
// are untouched on every failure; the caller publishes them to the rooted helper.
func (f *fn) emitCompactArrayAlloc(t uint32, zero bool, argSlot, resultSlot int) {
	layout, ok := f.compactArrayAllocLayout(t)
	if !ok {
		panic("amd64: invalid compact array bump layout")
	}
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		r := f.locals[local].reg
		if r >= R9 && r <= R11 {
			preserve[r-R9] = true
		}
	}
	for _, global := range f.globalPinIndices[:f.nGlobalPins] {
		r := globalRegValue(f.globalReg[global])
		if r >= R9 && r <= R11 {
			preserve[r-R9] = true
		}
	}
	a.Push(R8)
	delta := int32(8)
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
			delta += 8
		}
	}
	// Four private scratch words, inaccessible to helpers until restored.
	for i := 0; i < 4; i++ {
		a.Push(RDX)
	}
	delta += 32
	var fail []int
	guard := func(cond x64.Cond) { fail = append(fail, a.JccPlaceholder(cond)) }
	lengthSlot := argSlot
	if !zero {
		lengthSlot += layout.typ.stackSlots()
	}
	a.Load32(RCX, RSP, f.spillOff(lengthSlot)+delta)
	a.Store64(RSP, 16, RCX)
	a.ImulRI(RCX, int32(layout.size), true)
	a.AluRI(0, RCX, 23, true) // compact payload8 plus stride rounding15
	a.AluRI(4, RCX, -16, true)
	a.AluRI(cmpDigit, RCX, 64*1024, true)
	guard(condA)
	a.Store64(RSP, 0, RCX)
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	{
		a.TestSelf(R8, true)
		guard(condE)
		a.Load32(RAX, R8, gc.CompactInstanceVersionOffset)
		a.AluRI(cmpDigit, RAX, int32(gc.CompactNativeABIVersion), false)
		guard(condNE)
		a.Load32(RAX, R8, gc.CompactInstanceSizeOffset)
		a.AluRI(cmpDigit, RAX, gc.CompactInstanceViewABISize, false)
		guard(condNE)
		a.Load32(RAX, R8, gc.CompactInstanceLocalTypeCountOffset)
		a.AluRI(cmpDigit, RAX, int32(t), false)
		guard(condBE)
	}
	a.Load64(RAX, R8, gc.CompactInstanceLocalTypesOffset)
	{
		a.TestSelf(RAX, true)
		guard(condE)
	}
	a.Load32(RDX, RAX, int32(t*4)) // immutable admitted canonical descriptor identity
	a.Load64(R8, R8, gc.CompactInstanceCollectorOffset)
	{
		a.TestSelf(R8, true)
		guard(condE)
		a.Load32(RAX, R8, gc.CompactViewVersionOffset)
		a.AluRI(cmpDigit, RAX, int32(gc.CompactNativeABIVersion), false)
		guard(condNE)
		a.Load32(RAX, R8, gc.CompactViewSizeOffset)
		a.AluRI(cmpDigit, RAX, gc.CompactCollectorViewSize, false)
		guard(condNE)
	}
	a.Load32(RAX, R8, gc.CompactViewAllocationEnabledOffset)
	a.AluRI(cmpDigit, RAX, 1, false)
	guard(condNE)
	a.Load64(R9, R8, gc.CompactViewNurseryBumpAddressOffset)
	{
		a.TestSelf(R9, true)
		guard(condE)
	}
	a.Load32(R10, R9, 0)
	a.AluRM(cmpRMcode, R10, R8, gc.CompactViewNurseryBumpOffset, false)
	guard(condNE)
	{
		a.AluRM(cmpRMcode, R10, R8, gc.CompactViewNurseryStartOffset, false)
		guard(condB)
		a.TestSelf(R10, false)
		guard(condE)
		a.MovRegReg32(RAX, R10)
		a.AluRI(4, RAX, 15, false)
		a.TestSelf(RAX, false)
		guard(condNE)
	}
	a.Load32(RCX, R8, gc.CompactViewNurseryEndOffset)
	{
		a.AluRM(cmpRMcode, RCX, R8, gc.CompactViewArenaBytesOffset, false)
		guard(condA)
	}
	a.Cmp32(R10, RCX)
	guard(condA)
	a.Sub32(RCX, R10)
	a.AluRM(cmpRMcode, RCX, RSP, 0, true)
	guard(condB)
	a.Load64(R11, R8, gc.CompactViewArenaOffset)
	{
		a.TestSelf(R11, true)
		guard(condE)
	}
	a.Add64(R11, R10)
	a.MovRegReg32(RAX, R10)
	a.ShiftImm(5, RAX, 3, false)
	{
		a.AluRM(cmpRMcode, RAX, R8, gc.CompactViewStartCountOffset, false)
		guard(condAE)
	}
	a.Load64(RSI, R8, gc.CompactViewStartsOffset)
	{
		a.TestSelf(RSI, true)
		guard(condE)
	}
	a.Add64(RSI, RAX)
	{
		a.LoadIdx(RAX, RSI, RSP, 0, 1, false, false)
		a.TestSelf(RAX, false)
		guard(condNE)
	}
	a.MovRegReg32(RCX, R10)
	a.Load64(RAX, RSP, 0)
	a.Add64(RCX, RAX)
	a.Store64(RSP, 24, RCX)
	// No failure branch, call, trap, allocation or safepoint from first mutation through
	// publication. Serialized execution makes the short multiword commit atomic
	// with respect to the collector and other native activations.
	// The final 16B cover all padding, and precede logical element writes.
	a.Load64(RCX, RSP, 0)
	a.Add64(RCX, R11)
	a.MovImm32(RAX, 0)
	a.Store64(RCX, -16, RAX)
	a.Store64(RCX, -8, RAX)
	a.Store32(R11, 0, RDX)
	a.Load32(RCX, RSP, 16)
	a.Store32(R11, 4, RCX)
	a.MovReg64(RDX, R11)
	a.AluRI(0, RDX, 8, true)
	if zero {
		a.Load64(RCX, RSP, 0)
		a.AluRI(5, RCX, 8, true)
		a.ShiftImm(5, RCX, 3, true)
	} else if layout.size == 8 {
		a.Load64(RAX, RSP, f.spillOff(argSlot)+delta)
	} else {
		a.Load32(RAX, RSP, f.spillOff(argSlot)+delta)
	}
	a.TestSelf(RCX, true)
	empty := a.JccPlaceholder(condE)
	loop := a.Len()
	width := layout.size
	if zero {
		width = 8
	}
	a.StoreIdx(RDX, RSP, RAX, 0, width)
	a.AluRI(0, RDX, int32(width), true)
	a.AluRI(5, RCX, 1, true)
	again := a.JccPlaceholder(condNE)
	a.PatchRel32(again, loop)
	a.PatchRel32(empty, a.Len())
	a.Load32(RCX, RSP, 24)
	a.MovImm32(RAX, 1)
	a.StoreIdx(RSI, RSP, RAX, 0, 1)
	a.Store32(R9, 0, RCX)
	a.Store32(R8, gc.CompactViewNurseryBumpOffset, RCX)
	a.MovRegReg32(RAX, R10)
	a.Store64(RSP, f.spillOff(resultSlot)+delta, RAX)
	done := a.JmpPlaceholder()
	for _, site := range fail {
		a.PatchRel32(site, a.Len())
	}
	a.MovImm32(RAX, 0)
	a.PatchRel32(done, a.Len())
	a.TestSelf(RAX, false)
	for i := 0; i < 4; i++ {
		a.Pop(RDX)
	}
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Pop(R8)
}

func (f *fn) compactArrayAllocEligible(t uint32) bool {
	_, ok := f.compactArrayAllocLayout(t)
	return ok
}
