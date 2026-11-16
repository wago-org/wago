//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

func (f *fn) compactRefArrayChild(t uint32) (uint32, bool) {
	if !gcCompactRefStore || t > math.MaxInt32/4 {
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
	return child, ok && target.Final && (target.Comp.Kind == wasm.CompStruct || target.Comp.Kind == wasm.CompArray) && child <= math.MaxInt32/4
}

// This leaf never mutates the remembered queue or its dirty bytes. A clean old
// parent takes the complete checked helper before any payload mutation; only a
// young parent or an already registered dirty old parent can use the native store.
func (f *fn) emitCompactRefArraySet(t uint32, argSlot int) {
	child, ok := f.compactRefArrayChild(t)
	if !ok || !f.compactRefStoreEligible(t) {
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
	// The optional second resolution reuses only immutable view/backing checks
	// from the first. No call, safepoint or write intervenes. Each operand still
	// checks its own type-map bound, reference, start, region, type and full
	// physical extent. Reloading the same pointers avoids extra pinned state.
	resolve := func(typ uint32, slot int, reuseCheckedView bool) {
		header, payload, stride, width, valid := f.compactObjectGeometry(typ)
		if !valid {
			panic("amd64: invalid compact store geometry")
		}
		a.Load32(R10, RSP, f.spillOff(slot)+delta)
		a.TestSelf(R10, false)
		guard(condE)
		a.MovRegReg32(RAX, R10)
		a.AluRI(4, RAX, 15, false)
		a.TestSelf(RAX, false)
		guard(condNE)
		a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
		if !reuseCheckedView {
			a.TestSelf(R8, true)
			guard(condE)
			a.Load32(RAX, R8, gc.CompactInstanceVersionOffset)
			a.AluRI(cmpDigit, RAX, int32(gc.CompactNativeABIVersion), false)
			guard(condNE)
			a.Load32(RAX, R8, gc.CompactInstanceSizeOffset)
			a.AluRI(cmpDigit, RAX, gc.CompactInstanceViewABISize, false)
			guard(condNE)
		}
		a.Load32(RAX, R8, gc.CompactInstanceLocalTypeCountOffset)
		a.AluRI(cmpDigit, RAX, int32(typ), false)
		guard(condBE)
		a.Load64(RAX, R8, gc.CompactInstanceLocalTypesOffset)
		if !reuseCheckedView {
			a.TestSelf(RAX, true)
			guard(condE)
		}
		a.Load32(R11, RAX, int32(typ*4))
		a.Load64(R8, R8, gc.CompactInstanceCollectorOffset)
		if !reuseCheckedView {
			a.TestSelf(R8, true)
			guard(condE)
			a.Load32(RAX, R8, gc.CompactViewVersionOffset)
			a.AluRI(cmpDigit, RAX, int32(gc.CompactNativeABIVersion), false)
			guard(condNE)
			a.Load32(RAX, R8, gc.CompactViewSizeOffset)
			a.AluRI(cmpDigit, RAX, gc.CompactCollectorViewSize, false)
			guard(condNE)
		}
		a.AluRM(cmpRMcode, R10, R8, gc.CompactViewNurseryStartOffset, false)
		old := a.JccPlaceholder(condB)
		a.Load32(RCX, R8, gc.CompactViewNurseryBumpOffset)
		a.Cmp32(R10, RCX)
		young := a.JccPlaceholder(condB)
		a.PatchRel32(old, a.Len())
		a.AluRM(cmpRMcode, R10, R8, gc.CompactViewOldStartOffset, false)
		guard(condB)
		a.Load32(RCX, R8, gc.CompactViewOldBumpOffset)
		a.Cmp32(R10, RCX)
		guard(condAE)
		a.PatchRel32(young, a.Len())
		a.Sub32(RCX, R10)
		a.MovRegReg32(RAX, R10)
		a.ShiftImm(5, RAX, 3, false)
		a.AluRM(cmpRMcode, RAX, R8, gc.CompactViewStartCountOffset, false)
		guard(condAE)
		a.Load64(R9, R8, gc.CompactViewStartsOffset)
		if !reuseCheckedView {
			a.TestSelf(R9, true)
			guard(condE)
		}
		a.Add64(R9, RAX)
		a.LoadIdx(RAX, R9, RSP, 0, 1, false, false)
		a.AluRI(cmpDigit, RAX, 1, false)
		guard(condNE)
		a.Load32(RAX, R8, gc.CompactViewArenaBytesOffset)
		a.Cmp32(R10, RAX)
		guard(condA)
		a.Sub32(RAX, R10)
		a.AluRI(cmpDigit, RAX, int32(header), false)
		guard(condB)
		a.Load64(R9, R8, gc.CompactViewArenaOffset)
		if !reuseCheckedView {
			a.TestSelf(R9, true)
			guard(condE)
		}
		a.Add64(R9, R10)
		a.AluRI(cmpDigit, RCX, int32(header), false)
		guard(condB)
		a.Load32(RDX, R9, 0)
		a.Cmp32(RDX, R11)
		guard(condNE)
		if width != 0 {
			a.Load32(RDX, R9, 4)
			shift := uint8(0)
			for n := width; n > 1; n >>= 1 {
				shift++
			}
			if shift != 0 {
				a.ShiftImm(4, RDX, shift, true)
			}
			a.AluRI(0, RDX, int32(payload+15), true)
			a.AluRI(4, RDX, -16, true)
		} else {
			a.MovImm32(RDX, int32(stride))
		}
		a.Cmp64(RDX, RAX)
		guard(condA)
		a.Cmp64(RDX, RCX)
		guard(condA)

	}
	resolve(t, argSlot, false)
	// Validate complete logical length and selected physical slot before looking
	// at the child, preserving parent/bounds error precedence on the helper path.
	a.Load32(RAX, R9, 4)
	a.Load32(RCX, RSP, f.spillOff(argSlot+1)+delta)
	a.Cmp32(RCX, RAX)
	guard(condAE)
	a.ShiftImm(4, RCX, 2, true)
	a.LeaDisp(RDI, R9, 8)
	a.Add64(RDI, RCX)
	// The resolver already proved membership in nursery or active old space.
	a.AluRM(cmpRMcode, R10, R8, gc.CompactViewNurseryStartOffset, false)
	old := a.JccPlaceholder(condB)
	a.AluRM(cmpRMcode, R10, R8, gc.CompactViewNurseryBumpOffset, false)
	young := a.JccPlaceholder(condB)
	a.PatchRel32(old, a.Len())
	a.MovRegReg32(RAX, R10)
	a.ShiftImm(5, RAX, 3, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.CompactViewDirtyCountOffset, false)
	guard(condAE)
	a.Load64(RSI, R8, gc.CompactViewDirtyOffset)
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
	resolve(child, argSlot+2, gcCompactSharedChecks)
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

// Type geometry is compiler-owned and matches immutable collector descriptors.
func (f *fn) compactObjectGeometry(t uint32) (header, payload, stride, width uint32, ok bool) {
	st, found := f.stagedGCType(t)
	if !found || !st.Final {
		return
	}
	if st.Comp.Kind == wasm.CompArray {
		align, size, valid := nativeGCStorageLayout(f.m, t, st.Comp.Array.Storage())
		if !valid || align == 0 || size == 0 {
			return
		}
		return 8, (8 + align - 1) &^ (align - 1), 0, size, true
	}
	if st.Comp.Kind != wasm.CompStruct {
		return
	}
	layout, found := f.gcTypeLayout(t, wasm.CompStruct)
	if !found || layout.Align == 0 {
		return
	}
	align := uint64(layout.Align)
	var end uint64
	for _, field := range layout.FieldLayout {
		e := uint64(field.Offset) + uint64(field.Size)
		if e > end {
			end = e
		}
	}
	end = (end + align - 1) &^ (align - 1)
	p := (uint64(4) + align - 1) &^ (align - 1)
	s := (p + end + 15) &^ uint64(15)
	if s > math.MaxInt32 {
		return
	}
	return 4, uint32(p), uint32(s), 0, true
}
func (f *fn) compactRefStoreEligible(t uint32) bool {
	child, ok := f.compactRefArrayChild(t)
	if !ok {
		return false
	}
	_, _, _, _, ok = f.compactObjectGeometry(child)
	return ok
}
