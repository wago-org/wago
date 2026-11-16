//go:build amd64

package amd64

import (
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// A deliberately bounded code-size budget; unsupported constructors retain the
// checked moving helper, with no speculative reservation or elision.
func (f *fn) directOffsetStructAllocLayout(t uint32) ([]nativeGCStructAllocField, uint32, bool) {
	if !gcDirectAlloc || t > math.MaxInt32/4 {
		return nil, 0, false
	}
	st, found := f.stagedStructType(t)
	if !found || len(st.Comp.Fields) > 8 {
		return nil, 0, false
	}
	fields, size, _, pointerFree, ok := nativeGCStructAllocLayout(f.m, t)
	if !ok || !pointerFree || len(fields) > 8 || size > 128 {
		return nil, 0, false
	}
	for _, field := range fields {
		if field.ref || field.size != 4 && field.size != 8 {
			return nil, 0, false
		}
	}
	return fields, size, true
}

// Returns flags with ZF clear on success. Original canonical initializer slots
// are untouched on every failure; the caller publishes them to the rooted helper.
func (f *fn) emitDirectOffsetStructAlloc(t uint32, zero bool, argSlot, resultSlot int) {
	fields, size, ok := f.directOffsetStructAllocLayout(t)
	if !ok {
		panic("amd64: invalid direct bump layout")
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
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.TestSelf(R8, true)
	guard(condE)
	a.Load32(RAX, R8, gc.DirectInstanceVersionOffset)
	a.AluRI(cmpDigit, RAX, int32(gc.DirectABIVersion), false)
	guard(condNE)
	a.Load32(RAX, R8, gc.DirectInstanceSizeOffset)
	a.AluRI(cmpDigit, RAX, gc.DirectInstanceViewABISize, false)
	guard(condNE)
	a.Load32(RAX, R8, gc.DirectInstanceLocalTypeCountOffset)
	a.AluRI(cmpDigit, RAX, int32(t), false)
	guard(condBE)
	a.Load64(RAX, R8, gc.DirectInstanceLocalTypesOffset)
	a.TestSelf(RAX, true)
	guard(condE)
	a.Load32(RDX, RAX, int32(t*4)) // immutable admitted canonical descriptor identity
	a.Load64(R8, R8, gc.DirectInstanceCollectorOffset)
	a.TestSelf(R8, true)
	guard(condE)
	a.Load32(RAX, R8, gc.DirectViewVersionOffset)
	a.AluRI(cmpDigit, RAX, int32(gc.DirectABIVersion), false)
	guard(condNE)
	a.Load32(RAX, R8, gc.DirectViewSizeOffset)
	a.AluRI(cmpDigit, RAX, gc.DirectCollectorViewSize, false)
	guard(condNE)
	a.Load32(RAX, R8, gc.DirectViewAllocationEnabledOffset)
	a.AluRI(cmpDigit, RAX, 1, false)
	guard(condNE)
	a.Load64(R9, R8, gc.DirectViewNurseryBumpAddressOffset)
	a.TestSelf(R9, true)
	guard(condE)
	a.Load32(R10, R9, 0)
	a.AluRM(cmpRMcode, R10, R8, gc.DirectViewNurseryBumpOffset, false)
	guard(condNE)
	a.AluRM(cmpRMcode, R10, R8, gc.DirectViewNurseryStartOffset, false)
	guard(condB)
	a.TestSelf(R10, false)
	guard(condE)
	a.MovRegReg32(RAX, R10)
	a.AluRI(4, RAX, 15, false)
	a.TestSelf(RAX, false)
	guard(condNE)
	a.Load32(RCX, R8, gc.DirectViewNurseryEndOffset)
	a.AluRM(cmpRMcode, RCX, R8, gc.DirectViewArenaBytesOffset, false)
	guard(condA)
	a.Cmp32(R10, RCX)
	guard(condA)
	a.Sub32(RCX, R10)
	a.AluRI(cmpDigit, RCX, int32(stride), false)
	guard(condB)
	a.Load64(R11, R8, gc.DirectViewArenaOffset)
	a.TestSelf(R11, true)
	guard(condE)
	a.Add64(R11, R10)
	a.MovRegReg32(RAX, R10)
	a.ShiftImm(5, RAX, 3, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.DirectViewStartCountOffset, false)
	guard(condAE)
	a.Load64(RSI, R8, gc.DirectViewStartsOffset)
	a.TestSelf(RSI, true)
	guard(condE)
	a.Add64(RSI, RAX)
	a.LoadIdx(RAX, RSI, RSP, 0, 1, false, false)
	a.TestSelf(RAX, false)
	guard(condNE)
	a.Load64(RDI, R8, gc.DirectViewAllocationCounterOffset)
	a.TestSelf(RDI, true)
	guard(condE)
	a.Load64(RAX, RDI, 0)
	a.AluRI(cmpDigit, RAX, -1, true)
	guard(condE)
	a.MovRegReg32(RCX, R10)
	a.AluRI(0, RCX, int32(stride), false)
	// No branch, call, trap, allocation or safepoint from first mutation through
	// publication. Serialized execution makes the short multiword commit atomic
	// with respect to the collector and other native activations.
	a.Store32(R11, 0, RDX)
	a.StoreImm32Mem(R11, 4, int32(size))
	a.StoreImm32Mem(R11, 8, 0)
	a.StoreImm32Mem(R11, 12, 1) // pointer-free flag
	a.MovImm32(RAX, 0)
	for off := uint32(16); off < stride; off += 8 {
		a.Store64(R11, int32(off), RAX)
	}
	if !zero {
		for _, field := range fields {
			if field.size == 4 {
				a.Load32(RAX, RSP, f.spillOff(argSlot+int(field.slot))+delta)
				a.Store32(R11, int32(16+field.offset), RAX)
			} else {
				a.Load64(RAX, RSP, f.spillOff(argSlot+int(field.slot))+delta)
				a.Store64(R11, int32(16+field.offset), RAX)
			}
		}
	}
	a.MovImm32(RAX, 1)
	a.StoreIdx(RSI, RSP, RAX, 0, 1)
	a.Store32(R9, 0, RCX)
	a.Store32(R8, gc.DirectViewNurseryBumpOffset, RCX)
	a.Load64(RAX, RDI, 0)
	a.AluRI(0, RAX, 1, true)
	a.Store64(RDI, 0, RAX)
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
