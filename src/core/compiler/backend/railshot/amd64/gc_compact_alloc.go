//go:build amd64

package amd64

import (
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

func (f *fn) compactStructAllocEligible(t uint32) bool {
	_, _, _, ok := f.compactStructAllocLayout(t)
	return ok
}

// Layout16 only; bounded scalar constructors retain their entire helper fallback.
func (f *fn) compactStructAllocLayout(t uint32) ([]nativeGCStructAllocField, uint32, uint32, bool) {
	if !gcCompactAlloc || f.interruptible || t > math.MaxInt32/4 {
		return nil, 0, 0, false
	}
	st, found := f.stagedStructType(t)
	if !found || len(st.Comp.Fields) > 8 {
		return nil, 0, 0, false
	}
	fields, _, _, pointerFree, ok := nativeGCStructAllocLayout(f.m, t)
	if !ok || (!pointerFree && !gcCompactRefStruct) {
		return nil, 0, 0, false
	}
	align := uint32(1)
	end := uint32(0)
	for i, field := range fields {
		if field.ref {
			if !gcCompactRefStruct {
				return nil, 0, 0, false
			}
			if _, ok := f.compactStructChildType(t, uint32(i)); !ok {
				return nil, 0, 0, false
			}
		}
		if field.size != 4 && field.size != 8 {
			return nil, 0, 0, false
		}
		if field.size > align {
			align = field.size
		}
		end = field.offset + field.size
	}
	end = (end + align - 1) &^ (align - 1)
	payload := (uint32(4) + align - 1) &^ (align - 1)
	size := payload + end
	if size > 128 {
		return nil, 0, 0, false
	}
	return fields, payload, size, true
}

// Returns success through ZF. Every failure precedes mutation and leaves the
// canonical initializer/root slots untouched for the complete checked helper.
func (f *fn) emitCompactStructAlloc(t uint32, zero bool, argSlot, resultSlot int) {
	fields, payload, size, ok := f.compactStructAllocLayout(t)
	if !ok {
		panic("amd64: invalid compact bump layout")
	}
	stride := (size + 15) &^ uint32(15)
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
	var fail []int
	guard := func(cond x64.Cond) { fail = append(fail, a.JccPlaceholder(cond)) }
	// Child validation runs before parent bump/header/start publication. Re-read
	// canonical slots in the original helper on any failure or capacity miss.
	for i, field := range fields {
		if !field.ref {
			continue
		}
		if zero {
			if !field.nullable {
				fail = append(fail, a.JmpPlaceholder())
			}
			continue
		}
		child, valid := f.compactStructChildType(t, uint32(i))
		if !valid {
			panic("amd64: unchecked compact child")
		}
		null := -1
		if field.nullable {
			a.Load32(R10, RSP, f.spillOff(argSlot+int(field.slot))+delta)
			a.TestSelf(R10, false)
			null = a.JccPlaceholder(condE)
		}
		f.emitCompactConstructorChild(child, argSlot+int(field.slot), delta, guard)
		if null >= 0 {
			a.PatchRel32(null, a.Len())
		}
	}

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
	a.AluRI(cmpDigit, RCX, int32(stride), false)
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
	a.AluRI(0, RCX, int32(stride), false)
	// No branch, call, trap, allocation or safepoint from first mutation through
	// publication. Serialized execution makes the short multiword commit atomic
	// with respect to the collector and other native activations.
	// Clear the complete compact stride, including header-to-payload alignment
	// and tail padding, before writing the four-byte type header and fields.
	a.MovImm32(RAX, 0)
	for off := uint32(0); off < stride; off += 8 {
		a.Store64(R11, int32(off), RAX)
	}
	a.Store32(R11, 0, RDX)
	if !zero {
		for _, field := range fields {
			if field.size == 4 {
				a.Load32(RAX, RSP, f.spillOff(argSlot+int(field.slot))+delta)
				a.Store32(R11, int32(payload+field.offset), RAX)
			} else {
				a.Load64(RAX, RSP, f.spillOff(argSlot+int(field.slot))+delta)
				a.Store64(R11, int32(payload+field.offset), RAX)
			}
		}
	}
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
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Pop(R8)
}
