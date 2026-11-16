//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

func (f *fn) directOffsetRefArrayChild(t uint32) (uint32, bool) {
	if !gcDirectRefStore || t > math.MaxInt32/4 {
		return 0, false
	}
	st, ok := f.stagedArraySubtype(t)
	if !ok || !st.Final {
		return 0, false
	}
	storage := st.Comp.Array.Storage()
	v := storage.Val()
	if storage.Packed() || v.Kind() != wasm.ValRef || !v.Ref().Nullable() {
		return 0, false
	}
	heap := v.Ref().Heap()
	if heap.Kind() != wasm.HeapTypeIndex {
		return 0, false
	}
	child := heap.Type().Index
	if heap.Type().Rec {
		base, _, ok := nativeGCRecGroup(f.m, t)
		if !ok || child > math.MaxUint32-base {
			return 0, false
		}
		child += base
	}
	target, ok := f.stagedGCType(child)
	return child, ok && target.Final && target.Comp.Kind == wasm.CompStruct && child <= math.MaxInt32/4
}

// This leaf never mutates the remembered queue or its dirty bytes. A clean old
// parent takes the complete checked helper before any payload mutation; only a
// young parent or an already registered dirty old parent can use the native store.
func (f *fn) emitDirectOffsetRefArraySet(t uint32, argSlot int) {
	child, ok := f.directOffsetRefArrayChild(t)
	if !ok {
		panic("amd64: invalid direct reference store shape")
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
	var failures []int
	guard := func(c Cond) { failures = append(failures, a.JccPlaceholder(c)) }
	// Fixed-size two-object verification, never a descriptor/field-count walk.
	// Leaves ref in R10, object in R9, collector view in R8 and size in RDX.
	resolve := func(typ uint32, slot int) {
		a.Load32(R10, RSP, f.spillOff(slot)+delta)
		a.TestSelf(R10, false)
		guard(condE)
		a.MovRegReg32(RAX, R10)
		a.AluRI(4, RAX, 15, false)
		a.TestSelf(RAX, false)
		guard(condNE)
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
		a.AluRI(cmpDigit, RAX, int32(typ), false)
		guard(condBE)
		a.Load64(RAX, R8, gc.DirectInstanceLocalTypesOffset)
		a.TestSelf(RAX, true)
		guard(condE)
		a.Load32(R11, RAX, int32(typ*4))
		a.Load64(R8, R8, gc.DirectInstanceCollectorOffset)
		a.TestSelf(R8, true)
		guard(condE)
		a.Load32(RAX, R8, gc.DirectViewVersionOffset)
		a.AluRI(cmpDigit, RAX, int32(gc.DirectABIVersion), false)
		guard(condNE)
		a.Load32(RAX, R8, gc.DirectViewSizeOffset)
		a.AluRI(cmpDigit, RAX, gc.DirectCollectorViewSize, false)
		guard(condNE)
		a.AluRM(cmpRMcode, R10, R8, gc.DirectViewNurseryStartOffset, false)
		old := a.JccPlaceholder(condB)
		a.Load32(RCX, R8, gc.DirectViewNurseryBumpOffset)
		a.Cmp32(R10, RCX)
		young := a.JccPlaceholder(condB)
		a.PatchRel32(old, a.Len())
		a.AluRM(cmpRMcode, R10, R8, gc.DirectViewOldStartOffset, false)
		guard(condB)
		a.Load32(RCX, R8, gc.DirectViewOldBumpOffset)
		a.Cmp32(R10, RCX)
		guard(condAE)
		a.PatchRel32(young, a.Len())
		a.Sub32(RCX, R10)
		a.MovRegReg32(RAX, R10)
		a.ShiftImm(5, RAX, 3, false)
		a.AluRM(cmpRMcode, RAX, R8, gc.DirectViewStartCountOffset, false)
		guard(condAE)
		a.Load64(R9, R8, gc.DirectViewStartsOffset)
		a.TestSelf(R9, true)
		guard(condE)
		a.Add64(R9, RAX)
		a.LoadIdx(RAX, R9, RSP, 0, 1, false, false)
		a.AluRI(cmpDigit, RAX, 1, false)
		guard(condNE)
		a.Load32(RAX, R8, gc.DirectViewArenaBytesOffset)
		a.Cmp32(R10, RAX)
		guard(condA)
		a.Sub32(RAX, R10)
		a.AluRI(cmpDigit, RAX, 16, false)
		guard(condB)
		a.Load64(R9, R8, gc.DirectViewArenaOffset)
		a.TestSelf(R9, true)
		guard(condE)
		a.Add64(R9, R10)
		a.Load32(RDX, R9, 4)
		a.AluRI(cmpDigit, RDX, 16, false)
		guard(condB)
		a.Cmp32(RDX, RAX)
		guard(condA)
		a.Cmp32(RDX, RCX)
		guard(condA)
		a.Load32(RAX, R9, 0)
		a.Cmp32(RAX, R11)
		guard(condNE)
	}
	resolve(t, argSlot)
	// Validate complete logical length and selected physical slot before looking
	// at the child, preserving parent/bounds error precedence on the helper path.
	a.AluRI(5, RDX, 16, false)
	a.ShiftImm(5, RDX, 2, false)
	a.Load32(RAX, R9, 8)
	a.Cmp32(RAX, RDX)
	guard(condA)
	a.Load32(RCX, RSP, f.spillOff(argSlot+1)+delta)
	a.Cmp32(RCX, RAX)
	guard(condAE)
	a.ShiftImm(4, RCX, 2, true)
	a.LeaDisp(RDI, R9, 16)
	a.Add64(RDI, RCX)
	// The resolver already proved membership in nursery or active old space.
	a.AluRM(cmpRMcode, R10, R8, gc.DirectViewNurseryStartOffset, false)
	old := a.JccPlaceholder(condB)
	a.AluRM(cmpRMcode, R10, R8, gc.DirectViewNurseryBumpOffset, false)
	young := a.JccPlaceholder(condB)
	a.PatchRel32(old, a.Len())
	a.MovRegReg32(RAX, R10)
	a.ShiftImm(5, RAX, 3, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.DirectViewDirtyCountOffset, false)
	guard(condAE)
	a.Load64(RSI, R8, gc.DirectViewDirtyOffset)
	a.TestSelf(RSI, true)
	guard(condE)
	a.Add64(RSI, RAX)
	a.LoadIdx(RAX, RSI, RSP, 0, 1, false, false)
	a.AluRI(cmpDigit, RAX, 1, false)
	guard(condNE)
	a.PatchRel32(young, a.Len())
	a.Load32(R10, RSP, f.spillOff(argSlot+2)+delta)
	a.TestSelf(R10, false)
	null := a.JccPlaceholder(condE)
	resolve(child, argSlot+2)
	a.PatchRel32(null, a.Len())
	// All checks are complete; the one reference store cannot trigger collection.
	a.Store32(RDI, 0, R10)
	a.MovImm32(RAX, 1)
	done := a.JmpPlaceholder()
	for _, site := range failures {
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
