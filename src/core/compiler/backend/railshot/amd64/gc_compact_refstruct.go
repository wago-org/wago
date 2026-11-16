//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// Only indexed final collector types are eligible. Abstract/i31 and opaque
// storage retain the complete helper; final identity replaces subtype search.
func (f *fn) compactStructChildType(t, index uint32) (uint32, bool) {
	field, ok := f.stagedStructField(t, index)
	if !ok || field.Storage().Packed() || field.Storage().Val().Kind() != wasm.ValRef {
		return 0, false
	}
	heap := field.Storage().Val().Ref().Heap()
	if heap.Kind() != wasm.HeapTypeIndex {
		return 0, false
	}
	child := heap.Type().Index
	if heap.Type().Rec {
		base, _, found := nativeGCRecGroup(f.m, t)
		if !found || child > math.MaxUint32-base {
			return 0, false
		}
		child += base
	}
	target, ok := f.stagedGCType(child)
	if !ok || !target.Final || child > math.MaxInt32/4 || (target.Comp.Kind != wasm.CompStruct && target.Comp.Kind != wasm.CompArray) {
		return 0, false
	}
	_, _, _, _, ok = f.compactObjectGeometry(child)
	return child, ok
}

// Mirrors the checked compact ref-store resolver. Every failure branches to the
// original constructor helper before mutation, leaving canonical root slots intact.
// Clobbers only the existing allocator scratch set RAX/RCX/RDX/R8/R9/R10/R11.
func (f *fn) emitCompactConstructorChild(typ uint32, slot int, delta int32, guard func(Cond)) {
	a := f.a

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
	a.TestSelf(R8, true)
	guard(condE)
	a.Load32(RAX, R8, gc.CompactInstanceVersionOffset)
	a.AluRI(cmpDigit, RAX, int32(gc.CompactNativeABIVersion), false)
	guard(condNE)
	a.Load32(RAX, R8, gc.CompactInstanceSizeOffset)
	a.AluRI(cmpDigit, RAX, gc.CompactInstanceViewABISize, false)
	guard(condNE)
	a.Load32(RAX, R8, gc.CompactInstanceLocalTypeCountOffset)
	a.AluRI(cmpDigit, RAX, int32(typ), false)
	guard(condBE)
	a.Load64(RAX, R8, gc.CompactInstanceLocalTypesOffset)
	a.TestSelf(RAX, true)
	guard(condE)
	a.Load32(R11, RAX, int32(typ*4))
	a.Load64(R8, R8, gc.CompactInstanceCollectorOffset)
	a.TestSelf(R8, true)
	guard(condE)
	a.Load32(RAX, R8, gc.CompactViewVersionOffset)
	a.AluRI(cmpDigit, RAX, int32(gc.CompactNativeABIVersion), false)
	guard(condNE)
	a.Load32(RAX, R8, gc.CompactViewSizeOffset)
	a.AluRI(cmpDigit, RAX, gc.CompactCollectorViewSize, false)
	guard(condNE)
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
	a.TestSelf(R9, true)
	guard(condE)
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
	a.TestSelf(R9, true)
	guard(condE)
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
