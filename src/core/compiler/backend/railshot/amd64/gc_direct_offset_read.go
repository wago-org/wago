//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// This resolver intentionally retains no address certificate. Every access
// reloads the explicitly direct-offset view, including after moving helpers.
func (f *fn) emitCheckedDirectOffset(object *elem, typeIndex, required uint32) (Reg, func()) {
	ref := f.materialize(object)
	f.pinned = f.pinned.add(ref)
	view := f.allocReg(maskOf(ref))
	f.pinned = f.pinned.add(view)
	tmp := f.allocReg(maskOf(ref, view))
	f.pinned = f.pinned.add(tmp)
	f.a.TestSelf(ref, false)
	f.trapIf(condE, trapNullReference)
	f.a.MovRegReg32(ref, ref) // compact reference is unsigned
	f.a.MovRegReg32(tmp, ref)
	f.a.AluRI(4, tmp, 15, false)
	f.a.TestSelf(tmp, false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load64(view, RBX, -int32(abi.GCNativeViewPtrOffset))
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Load32(tmp, view, gc.DirectInstanceVersionOffset)
	f.a.AluRI(cmpDigit, tmp, int32(gc.DirectABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load32(tmp, view, gc.DirectInstanceSizeOffset)
	f.a.AluRI(cmpDigit, tmp, gc.DirectInstanceViewABISize, false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load32(tmp, view, gc.DirectInstanceLocalTypeCountOffset)
	f.a.AluRI(cmpDigit, tmp, int32(typeIndex), false)
	f.trapIf(condBE, trapCastFailure)
	f.a.Load64(tmp, view, gc.DirectInstanceLocalTypesOffset)
	f.a.TestSelf(tmp, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Load32(tmp, tmp, int32(typeIndex*4))
	oldFloor := f.spillFloor
	slot := f.allocSpillSlots(3)
	f.spillFloor = slot + 3
	f.a.StoreRsp32(f.spillOff(slot), tmp)
	f.a.Load64(view, view, gc.DirectInstanceCollectorOffset)
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Load32(tmp, view, gc.DirectViewVersionOffset)
	f.a.AluRI(cmpDigit, tmp, int32(gc.DirectABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load32(tmp, view, gc.DirectViewSizeOffset)
	f.a.AluRI(cmpDigit, tmp, gc.DirectCollectorViewSize, false)
	f.trapIf(condNE, trapCastFailure)
	// Membership in a currently active allocation range is independent of the
	// starts table: an inactive semispace can still contain historical headers.
	f.a.AluRM(cmpRMcode, ref, view, gc.DirectViewNurseryStartOffset, false)
	old := f.a.JccPlaceholder(condB)
	f.a.Load32(tmp, view, gc.DirectViewNurseryBumpOffset)
	f.a.Cmp32(ref, tmp)
	young := f.a.JccPlaceholder(condB)
	f.a.PatchRel32(old, f.a.Len())
	f.a.AluRM(cmpRMcode, ref, view, gc.DirectViewOldStartOffset, false)
	f.trapIf(condB, trapCastFailure)
	f.a.Load32(tmp, view, gc.DirectViewOldBumpOffset)
	f.a.Cmp32(ref, tmp)
	f.trapIf(condAE, trapCastFailure)
	f.a.PatchRel32(young, f.a.Len())
	f.a.Sub32(tmp, ref)
	f.a.StoreRsp32(f.spillOff(slot+2), tmp)
	f.a.MovRegReg32(tmp, ref)
	f.a.ShiftImm(5, tmp, 3, false)
	f.a.AluRM(cmpRMcode, tmp, view, gc.DirectViewStartCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	f.a.StoreRsp64(f.spillOff(slot+1), view)
	f.a.Load64(view, view, gc.DirectViewStartsOffset)
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Add64(view, tmp)
	f.a.LoadIdx(tmp, view, RSP, 0, 1, false, false)
	f.a.AluRI(cmpDigit, tmp, 1, false)
	f.trapIf(condNE, trapCastFailure)
	f.a.LoadRsp64(view, f.spillOff(slot+1))
	f.a.Load32(tmp, view, gc.DirectViewArenaBytesOffset)
	f.a.Cmp32(ref, tmp)
	f.trapIf(condA, trapCastFailure)
	f.a.Sub32(tmp, ref)
	f.a.AluRI(cmpDigit, tmp, int32(required), false)
	f.trapIf(condB, trapCastFailure)
	f.a.StoreRsp32(f.spillOff(slot+1), tmp) // maximum remaining physical bytes
	f.a.Load64(view, view, gc.DirectViewArenaOffset)
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Add64(view, ref)
	f.a.Load32(tmp, view, 4)
	f.a.AluRI(cmpDigit, tmp, int32(required), false)
	f.trapIf(condB, trapCastFailure)
	f.a.LoadRsp32(ref, f.spillOff(slot+1))
	f.a.Cmp32(tmp, ref)
	f.trapIf(condA, trapCastFailure)
	f.a.LoadRsp32(ref, f.spillOff(slot+2))
	f.a.Cmp32(tmp, ref)
	f.trapIf(condA, trapCastFailure)
	f.a.Load32(tmp, view, 0)
	f.a.LoadRsp32(ref, f.spillOff(slot))
	f.a.Cmp32(tmp, ref)
	f.trapIf(condNE, trapCastFailure)
	f.pinned = f.pinned.remove(ref)
	f.release(ref)
	f.pinned = f.pinned.remove(tmp)
	f.spillFloor = oldFloor
	return view, func() { f.pinned = f.pinned.remove(view) }
}

func (f *fn) emitDirectOffsetStructGet(typeIndex, fieldIndex, helper uint32) bool {
	off, scalar, final, ok := f.directGCStructLayout(typeIndex, fieldIndex)
	if !gcDirectReads || !ok || !final || typeIndex > math.MaxInt32/4 {
		return false
	}
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	obj, done := f.emitCheckedDirectOffset(f.popValue(), typeIndex, gc.PayloadOffset+off+uint32(scalar.size))
	if scalar.typ.isFloat() {
		x := f.allocFReg(0)
		f.a.FLoadDisp(x, obj, int32(gc.PayloadOffset+off), scalar.typ == mtF64)
		done()
		f.spillFloor = floor
		f.pushFReg(x, scalar.typ)
	} else {
		r := f.allocReg(maskOf(obj))
		f.a.LoadIdx(r, obj, RSP, int32(gc.PayloadOffset+off), scalar.size, helper == gcStructGetS, scalar.typ == mtI64)
		done()
		f.spillFloor = floor
		f.pushReg(r, scalar.typ)
	}
	return true
}

func (f *fn) emitDirectOffsetArrayLen(typeIndex uint32) bool {
	target, ok := f.stagedArraySubtype(typeIndex)
	if !gcDirectReads || !ok || !target.Final || target.Comp.Kind != wasm.CompArray || typeIndex > math.MaxInt32/4 {
		return false
	}
	_, elemSize, valid := nativeGCStorageLayout(f.m, typeIndex, target.Comp.Array.Storage())
	if !valid || elemSize == 0 {
		return false
	}
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	obj, done := f.emitCheckedDirectOffset(f.popValue(), typeIndex, gc.HeaderSize)
	r := f.allocReg(maskOf(obj))
	f.pinned = f.pinned.add(r)
	f.a.Load32(r, obj, 8)
	tmp := f.allocReg(maskOf(obj, r))
	f.a.Load32(tmp, obj, 4)
	f.a.AluRI(5, tmp, int32(gc.HeaderSize), false)
	shift := uint8(0)
	for n := elemSize; n > 1; n >>= 1 {
		shift++
	}
	if shift != 0 {
		f.a.ShiftImm(5, tmp, shift, false)
	}
	f.a.Cmp32(r, tmp)
	f.trapIf(condA, trapCastFailure)
	f.pinned = f.pinned.remove(r)
	done()
	f.spillFloor = floor
	f.pushReg(r, mtI32)
	return true
}
