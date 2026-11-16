//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// Layout16 only. Every read reloads the typed compact view; no address survives a helper.
func (f *fn) emitCheckedCompactOffset(object *elem, typeIndex, required, stride, payload, elemSize uint32) (Reg, func()) {
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
	f.a.Load32(tmp, view, gc.CompactInstanceVersionOffset)
	f.a.AluRI(cmpDigit, tmp, int32(gc.CompactNativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load32(tmp, view, gc.CompactInstanceSizeOffset)
	f.a.AluRI(cmpDigit, tmp, gc.CompactInstanceViewABISize, false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load32(tmp, view, gc.CompactInstanceLocalTypeCountOffset)
	f.a.AluRI(cmpDigit, tmp, int32(typeIndex), false)
	f.trapIf(condBE, trapCastFailure)
	f.a.Load64(tmp, view, gc.CompactInstanceLocalTypesOffset)
	f.a.TestSelf(tmp, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Load32(tmp, tmp, int32(typeIndex*4))
	oldFloor := f.spillFloor
	slot := f.allocSpillSlots(3)
	f.spillFloor = slot + 3
	f.a.StoreRsp32(f.spillOff(slot), tmp)
	f.a.Load64(view, view, gc.CompactInstanceCollectorOffset)
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Load32(tmp, view, gc.CompactViewVersionOffset)
	f.a.AluRI(cmpDigit, tmp, int32(gc.CompactNativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load32(tmp, view, gc.CompactViewSizeOffset)
	f.a.AluRI(cmpDigit, tmp, gc.CompactCollectorViewSize, false)
	f.trapIf(condNE, trapCastFailure)
	// Membership in a currently active allocation range is independent of the
	// starts table: an inactive semispace can still contain historical headers.
	f.a.AluRM(cmpRMcode, ref, view, gc.CompactViewNurseryStartOffset, false)
	old := f.a.JccPlaceholder(condB)
	f.a.Load32(tmp, view, gc.CompactViewNurseryBumpOffset)
	f.a.Cmp32(ref, tmp)
	young := f.a.JccPlaceholder(condB)
	f.a.PatchRel32(old, f.a.Len())
	f.a.AluRM(cmpRMcode, ref, view, gc.CompactViewOldStartOffset, false)
	f.trapIf(condB, trapCastFailure)
	f.a.Load32(tmp, view, gc.CompactViewOldBumpOffset)
	f.a.Cmp32(ref, tmp)
	f.trapIf(condAE, trapCastFailure)
	f.a.PatchRel32(young, f.a.Len())
	f.a.Sub32(tmp, ref)
	f.a.StoreRsp32(f.spillOff(slot+2), tmp)
	f.a.MovRegReg32(tmp, ref)
	f.a.ShiftImm(5, tmp, 3, false)
	f.a.AluRM(cmpRMcode, tmp, view, gc.CompactViewStartCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	f.a.StoreRsp64(f.spillOff(slot+1), view)
	f.a.Load64(view, view, gc.CompactViewStartsOffset)
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Add64(view, tmp)
	f.a.LoadIdx(tmp, view, RSP, 0, 1, false, false)
	f.a.AluRI(cmpDigit, tmp, 1, false)
	f.trapIf(condNE, trapCastFailure)
	f.a.LoadRsp64(view, f.spillOff(slot+1))
	f.a.Load32(tmp, view, gc.CompactViewArenaBytesOffset)
	f.a.Cmp32(ref, tmp)
	f.trapIf(condA, trapCastFailure)
	f.a.Sub32(tmp, ref)
	f.a.AluRI(cmpDigit, tmp, int32(required), false)
	f.trapIf(condB, trapCastFailure)
	f.a.StoreRsp32(f.spillOff(slot+1), tmp) // maximum remaining physical bytes
	f.a.Load64(view, view, gc.CompactViewArenaOffset)
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Add64(view, ref)
	// No Size word exists. Exact immutable type identity selects the geometry.
	f.a.LoadRsp32(ref, f.spillOff(slot+2))
	f.a.AluRI(cmpDigit, ref, int32(required), false)
	f.trapIf(condB, trapCastFailure)
	f.a.Load32(tmp, view, 0)
	f.a.LoadRsp32(ref, f.spillOff(slot))
	f.a.Cmp32(tmp, ref)
	f.trapIf(condNE, trapCastFailure)
	if elemSize != 0 {
		f.a.Load32(tmp, view, 4)
		shift := uint8(0)
		for n := elemSize; n > 1; n >>= 1 {
			shift++
		}
		if shift != 0 {
			f.a.ShiftImm(4, tmp, shift, true)
		}
		f.a.AluRI(0, tmp, int32(payload+15), true)
		f.a.AluRI(4, tmp, -16, true)
	} else {
		f.a.MovImm32(tmp, int32(stride))
	}
	f.a.LoadRsp32(ref, f.spillOff(slot+1))
	f.a.Cmp64(tmp, ref)
	f.trapIf(condA, trapCastFailure)
	f.a.LoadRsp32(ref, f.spillOff(slot+2))
	f.a.Cmp64(tmp, ref)
	f.trapIf(condA, trapCastFailure)
	f.pinned = f.pinned.remove(ref)
	f.release(ref)
	f.pinned = f.pinned.remove(tmp)
	f.spillFloor = oldFloor
	return view, func() { f.pinned = f.pinned.remove(view) }
}

func (f *fn) emitCompactStructGet(t, fieldIndex, helper uint32) bool {
	off, scalar, final, ok := f.directGCStructLayout(t, fieldIndex)
	layout, found := f.gcTypeLayout(t, wasm.CompStruct)
	reference := false
	if !ok && found && helper == gcStructGet && int(fieldIndex) < len(layout.FieldLayout) {
		field, present := f.stagedStructField(t, fieldIndex)
		fl := layout.FieldLayout[fieldIndex]
		if present && fl.Size == 4 && nativeGCCollectorRefStorage(f.m, t, field.Storage()) {
			off, scalar, final, ok = fl.Offset, directGCScalar{size: 4, typ: mtI64}, layout.Type.Final, true
			reference = true
		}
	}
	if !gcCompactReads || !ok || !final || !found || layout.Align == 0 || t > math.MaxInt32/4 {
		return false
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
	payload := (uint64(4) + align - 1) &^ (align - 1)
	stride := (payload + end + 15) &^ uint64(15)
	if stride > math.MaxInt32 || payload+uint64(off)+uint64(scalar.size) > stride {
		return false
	}
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	obj, done := f.emitCheckedCompactOffset(f.popValue(), t, 4, uint32(stride), 0, 0)
	if reference {
		f.stats.peep("gc-compact-struct-ref-read")
	} else {
		f.stats.peep("gc-compact-struct-read")
	}
	disp := int32(payload + uint64(off))
	if scalar.typ.isFloat() {
		x := f.allocFReg(0)
		f.a.FLoadDisp(x, obj, disp, scalar.typ == mtF64)
		done()
		f.spillFloor = floor
		f.pushFReg(x, scalar.typ)
	} else {
		r := f.allocReg(maskOf(obj))
		if reference {
			f.a.Load32(r, obj, disp)
		} else {
			f.a.LoadIdx(r, obj, RSP, disp, scalar.size, helper == gcStructGetS, scalar.typ == mtI64)
		}
		done()
		f.spillFloor = floor
		f.pushReg(r, scalar.typ)
		if reference {
			// Like the checked helper, this loads the stored compact reference
			// without revalidating its declared child type. All receiver checks
			// remain above. The value must remain an exact root across helpers.
			f.markTopGCReference()
		}
	}
	return true
}

func (f *fn) emitCompactArrayLen(t uint32) bool {
	target, ok := f.stagedArraySubtype(t)
	if !gcCompactReads || !ok || !target.Final || target.Comp.Kind != wasm.CompArray || t > math.MaxInt32/4 {
		return false
	}
	align, size, ok := nativeGCStorageLayout(f.m, t, target.Comp.Array.Storage())
	if !ok || align == 0 || size == 0 {
		return false
	}
	payload := (uint32(8) + align - 1) &^ (align - 1)
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	obj, done := f.emitCheckedCompactOffset(f.popValue(), t, 8, 0, payload, size)
	r := f.allocReg(maskOf(obj))
	f.a.Load32(r, obj, 4)
	f.stats.peep("gc-compact-array-len")
	done()
	f.spillFloor = floor
	f.pushReg(r, mtI32)
	return true
}
