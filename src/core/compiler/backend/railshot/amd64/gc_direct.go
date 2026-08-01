//go:build amd64

package amd64

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// directGCScalar describes a pointer-free scalar storage location that can be
// accessed without entering the parked Go helper. Reference and v128 storage
// deliberately remain helper-bound until their barrier/vector paths are proven.
type directGCScalar struct {
	size int
	typ  machineType
}

func directGCScalarStorage(st wasm.StorageType) (directGCScalar, bool) {
	if st.Packed {
		switch st.Pack {
		case wasm.PackI8:
			return directGCScalar{size: 1, typ: mtI32}, true
		case wasm.PackI16:
			return directGCScalar{size: 2, typ: mtI32}, true
		default:
			return directGCScalar{}, false
		}
	}
	if st.Val.Kind != wasm.ValNum {
		return directGCScalar{}, false
	}
	switch st.Val.Num {
	case wasm.NumI32:
		return directGCScalar{size: 4, typ: mtI32}, true
	case wasm.NumI64:
		return directGCScalar{size: 8, typ: mtI64}, true
	case wasm.NumF32:
		return directGCScalar{size: 4, typ: mtF32}, true
	case wasm.NumF64:
		return directGCScalar{size: 8, typ: mtF64}, true
	default:
		return directGCScalar{}, false
	}
}

func nativeGCStorageLayout(m *wasm.Module, containingType uint32, st wasm.StorageType) (align, size uint32, ok bool) {
	if st.Packed {
		switch st.Pack {
		case wasm.PackI8:
			return 1, 1, true
		case wasm.PackI16:
			return 2, 2, true
		default:
			return 0, 0, false
		}
	}
	switch st.Val.Kind {
	case wasm.ValNum:
		switch st.Val.Num {
		case wasm.NumI32, wasm.NumF32:
			return 4, 4, true
		case wasm.NumI64, wasm.NumF64:
			return 8, 8, true
		}
	case wasm.ValVec:
		return 16, 16, true
	case wasm.ValRef:
		h := st.Val.Ref.Heap
		if h.Kind == wasm.HeapAbs {
			switch h.Abs {
			case wasm.HeapFunc, wasm.HeapNoFunc, wasm.HeapExtern, wasm.HeapNoExtern:
				return 8, 8, true
			default:
				return 4, 4, true
			}
		}
		if h.Kind == wasm.HeapDefType && h.Def != nil {
			if int(h.Def.Index) < len(h.Def.Rec.SubTypes) && h.Def.Rec.SubTypes[h.Def.Index].Comp.Kind == wasm.CompFunc {
				return 8, 8, true
			}
			return 4, 4, true
		}
		if h.Kind == wasm.HeapTypeIndex {
			target := h.Type.Index
			if h.Type.Rec {
				base, _, found := nativeGCRecGroup(m, containingType)
				if !found {
					return 0, 0, false
				}
				target = base + target
			}
			if targetType, found := nativeGCFlatType(m, target); found && targetType.Comp.Kind == wasm.CompFunc {
				return 8, 8, true
			}
			return 4, 4, true
		}
	}
	return 0, 0, false
}

func nativeGCFlatType(m *wasm.Module, index uint32) (wasm.SubType, bool) {
	if m == nil {
		return wasm.SubType{}, false
	}
	for _, group := range m.Types {
		if index < uint32(len(group.SubTypes)) {
			return group.SubTypes[index], true
		}
		index -= uint32(len(group.SubTypes))
	}
	return wasm.SubType{}, false
}

func nativeGCRecGroup(m *wasm.Module, index uint32) (base, length uint32, ok bool) {
	if m == nil {
		return 0, 0, false
	}
	var cursor uint32
	for _, group := range m.Types {
		n := uint32(len(group.SubTypes))
		if index >= cursor && index < cursor+n {
			return cursor, n, true
		}
		cursor += n
	}
	return 0, 0, false
}

func directGCStructLayout(m *wasm.Module, typeIndex, fieldIndex uint32) (payloadOffset uint32, scalar directGCScalar, final bool, ok bool) {
	st, found := nativeGCFlatType(m, typeIndex)
	if !found || st.Comp.Kind != wasm.CompStruct || fieldIndex >= uint32(len(st.Comp.Fields)) {
		return 0, directGCScalar{}, false, false
	}
	scalar, ok = directGCScalarStorage(st.Comp.Fields[fieldIndex].Storage)
	if !ok {
		return 0, directGCScalar{}, false, false
	}
	var off uint32
	for i, field := range st.Comp.Fields {
		align, size, valid := nativeGCStorageLayout(m, typeIndex, field.Storage)
		if !valid || align == 0 || off > math.MaxUint32-(align-1) {
			return 0, directGCScalar{}, false, false
		}
		off = (off + align - 1) &^ (align - 1)
		if uint32(i) == fieldIndex {
			return off, scalar, st.Final, true
		}
		if off > math.MaxUint32-size {
			return 0, directGCScalar{}, false, false
		}
		off += size
	}
	return 0, directGCScalar{}, false, false
}

func directGCArrayLayout(m *wasm.Module, typeIndex uint32) (directGCScalar, bool, bool) {
	st, found := nativeGCFlatType(m, typeIndex)
	if !found || st.Comp.Kind != wasm.CompArray {
		return directGCScalar{}, false, false
	}
	scalar, ok := directGCScalarStorage(st.Comp.Array.Storage)
	return scalar, st.Final, ok
}

// emitCheckedGCObject resolves one compact object handle through the stable
// versioned collector view and proves exact canonical type plus object bounds.
// The caller must have raised spillFloor so consumed operand slots cannot be
// reused. Returned obj is pinned scratch and must be released with done.
func (f *fn) emitCheckedGCObject(object *elem, localType, requiredBytes uint32) (obj Reg, done func()) {
	ref := f.materialize(object)
	f.pinned = f.pinned.add(ref)
	f.a.TestSelf(ref, false)
	f.trapIf(condE, trapNullReference)

	view := f.allocReg(maskOf(ref))
	f.pinned = f.pinned.add(view)
	tmp := f.allocReg(maskOf(ref, view))
	f.pinned = f.pinned.add(tmp)

	// Reject i31 immediates and malformed handles before indexing metadata.
	f.a.MovRegReg32(tmp, ref)
	f.a.AluRI(4, tmp, 1, false)
	f.a.TestSelf(tmp, false)
	f.trapIf(condNE, trapCastFailure)

	f.a.Load64(view, RBX, -int32(abi.GCNativeViewPtrOffset))
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Load32(tmp, view, gc.NativeInstanceViewVersionOffset)
	f.a.AluRI(cmpDigit, tmp, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load32(tmp, view, gc.NativeInstanceViewLocalTypeCountOffset)
	f.a.AluRI(cmpDigit, tmp, int32(localType), false)
	f.trapIf(condBE, trapCastFailure)
	f.a.Load64(tmp, view, gc.NativeInstanceViewLocalTypesOffset)
	if uint64(localType)*4 > math.MaxInt32 {
		panic("amd64: native GC local type table displacement overflow")
	}
	f.a.Load32(tmp, tmp, int32(localType*4))

	oldFloor := f.spillFloor
	domainSlot := f.allocSpillSlots(2)
	f.spillFloor = domainSlot + 2
	f.a.StoreRsp32(f.spillOff(domainSlot), tmp)

	f.a.Load64(view, view, gc.NativeInstanceViewCollectorOffset)
	f.a.TestSelf(view, true)
	f.trapIf(condE, trapCastFailure)
	f.a.Load32(tmp, view, gc.NativeViewVersionOffset)
	f.a.AluRI(cmpDigit, tmp, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	f.a.Load32(tmp, view, gc.NativeViewHandleStrideOffset)
	f.a.AluRI(cmpDigit, tmp, gc.NativeHandleStride, false)
	f.trapIf(condNE, trapCastFailure)

	f.a.ShiftImm(5, ref, 1, false) // compact handle index
	// cmp ref,[view+HandleCount]; h >= count is invalid.
	f.a.AluRM(cmpRMcode, ref, view, gc.NativeViewHandleCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	f.a.Load64(tmp, view, gc.NativeViewHandlesOffset)
	f.a.ImulRI(ref, gc.NativeHandleStride, true)
	f.a.Add64(tmp, ref) // tmp = handle entry

	// Extract the one-byte space from the final dword of the 20-byte entry.
	f.a.Load32(ref, tmp, 16)
	f.a.ShiftImm(5, ref, 16, false)
	f.a.AluRI(4, ref, 0xff, false)
	f.a.TestSelf(ref, false)
	f.trapIf(condE, trapCastFailure)
	f.a.AluRI(cmpDigit, ref, gc.NativeSpaceCount-1, false)
	f.trapIf(condA, trapCastFailure)
	f.a.ImulRI(ref, gc.NativeViewSpaceStride, true)
	f.a.LeaDisp(view, view, gc.NativeViewSpacesOffset)
	f.a.Add64(view, ref) // view = selected NativeSpaceView
	f.a.Load64(ref, view, gc.NativeSpaceBaseOffset)
	f.a.StoreRsp64(f.spillOff(domainSlot+1), ref) // stable heap base
	f.a.Load32(view, view, gc.NativeSpaceBytesOffset)

	// Validate entry range against the selected heap and against this access.
	f.a.Load32(ref, tmp, gc.NativeHandleOffsetOffset)
	f.a.Cmp32(ref, view)
	f.trapIf(condA, trapCastFailure)
	f.a.Sub32(view, ref) // bytes remaining from object offset
	f.a.Load32(ref, tmp, gc.NativeHandleSizeOffset)
	f.a.Cmp32(ref, view)
	f.trapIf(condA, trapCastFailure)
	f.a.AluRI(cmpDigit, ref, int32(requiredBytes), false)
	f.trapIf(condB, trapCastFailure)

	f.a.Load64(view, RSP, f.spillOff(domainSlot+1))
	f.a.Load32(tmp, tmp, gc.NativeHandleOffsetOffset)
	f.a.Add64(view, tmp) // view = object header
	f.a.Load32(ref, view, 0)
	f.a.LoadRsp32(tmp, f.spillOff(domainSlot))
	f.a.Cmp32(ref, tmp)
	f.trapIf(condNE, trapCastFailure)

	f.pinned = f.pinned.remove(ref)
	f.release(ref)
	f.pinned = f.pinned.remove(tmp)
	// tmp is scratch, not an occupied value.
	f.spillFloor = oldFloor
	return view, func() {
		f.pinned = f.pinned.remove(view)
	}
}

func (f *fn) emitDirectGCStructGet(typeIndex, fieldIndex uint32, helper uint32) bool {
	off, scalar, final, ok := directGCStructLayout(f.m, typeIndex, fieldIndex)
	if !ok || !final {
		return false
	}
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	object := f.popValue()
	required := gc.PayloadOffset + off + uint32(scalar.size)
	obj, done := f.emitCheckedGCObject(object, typeIndex, required)
	disp := int32(gc.PayloadOffset + off)
	if scalar.typ.isFloat() {
		x := f.allocFReg(0)
		f.a.FLoadDisp(x, obj, disp, scalar.typ == mtF64)
		done()
		f.spillFloor = oldFloor
		f.pushFReg(x, scalar.typ)
		return true
	}
	result := f.allocReg(maskOf(obj))
	signed := helper == gcStructGetS
	f.a.LoadIdx(result, obj, RSP, disp, scalar.size, signed, scalar.typ == mtI64)
	done()
	f.spillFloor = oldFloor
	f.pushReg(result, scalar.typ)
	return true
}

func (f *fn) emitDirectGCStructSet(typeIndex, fieldIndex uint32) bool {
	off, scalar, final, ok := directGCStructLayout(f.m, typeIndex, fieldIndex)
	if !ok || !final {
		return false
	}
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	value := f.popValue()
	object := f.popValue()
	required := gc.PayloadOffset + off + uint32(scalar.size)
	obj, done := f.emitCheckedGCObject(object, typeIndex, required)
	disp := int32(gc.PayloadOffset + off)
	if scalar.typ.isFloat() {
		x := f.materializeF(value)
		f.a.FStoreDisp(obj, disp, x, scalar.typ == mtF64)
		f.releaseF(x)
	} else {
		r := f.materialize(value)
		f.a.StoreIdx(obj, RSP, r, disp, scalar.size)
		f.release(r)
	}
	done()
	f.spillFloor = oldFloor
	return true
}

func (f *fn) emitDirectGCArrayGet(typeIndex uint32, helper uint32) bool {
	scalar, final, ok := directGCArrayLayout(f.m, typeIndex)
	if !ok || !final {
		return false
	}
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	indexValue := f.popValue()
	object := f.popValue()
	obj, done := f.emitCheckedGCObject(object, typeIndex, gc.PayloadOffset)
	index := f.materialize(indexValue)
	f.pinned = f.pinned.add(index)
	tmp := f.allocReg(maskOf(obj, index))
	f.pinned = f.pinned.add(tmp)
	f.a.Load32(tmp, obj, 8) // ObjHeader.Aux array length
	f.a.Cmp32(index, tmp)
	f.trapIf(condAE, trapCastFailure)
	f.a.ImulRI(index, int32(scalar.size), true)
	// Defend against corrupted Aux metadata as well as the logical index check:
	// payload offset + scaled index + element width must fit the object header size.
	f.a.Load32(tmp, obj, 4)
	end := f.allocReg(maskOf(obj, index, tmp))
	f.pinned = f.pinned.add(end)
	f.a.MovReg64(end, index)
	f.a.AluRI(0, end, int32(gc.PayloadOffset)+int32(scalar.size), true)
	f.a.Cmp64(end, tmp)
	f.trapIf(condA, trapCastFailure)
	if scalar.typ.isFloat() {
		x := f.allocFReg(0)
		f.a.FLoadIdx(x, obj, index, int32(gc.PayloadOffset), scalar.typ == mtF64)
		f.pinned = f.pinned.remove(end)
		f.pinned = f.pinned.remove(tmp)
		f.pinned = f.pinned.remove(index)
		f.release(index)
		done()
		f.spillFloor = oldFloor
		f.pushFReg(x, scalar.typ)
		return true
	}
	f.pinned = f.pinned.remove(tmp)
	f.pinned = f.pinned.remove(end)
	result := end
	signed := helper == gcArrayGetS
	f.a.LoadIdx(result, obj, index, int32(gc.PayloadOffset), scalar.size, signed, scalar.typ == mtI64)
	f.pinned = f.pinned.remove(index)
	f.release(index)
	done()
	f.spillFloor = oldFloor
	f.pushReg(result, scalar.typ)
	return true
}

func (f *fn) emitDirectGCArraySet(typeIndex uint32) bool {
	scalar, final, ok := directGCArrayLayout(f.m, typeIndex)
	if !ok || !final {
		return false
	}
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	value := f.popValue()
	indexValue := f.popValue()
	object := f.popValue()
	obj, done := f.emitCheckedGCObject(object, typeIndex, gc.PayloadOffset)
	index := f.materialize(indexValue)
	f.pinned = f.pinned.add(index)
	tmp := f.allocReg(maskOf(obj, index))
	f.pinned = f.pinned.add(tmp)
	f.a.Load32(tmp, obj, 8)
	f.a.Cmp32(index, tmp)
	f.trapIf(condAE, trapCastFailure)
	f.a.ImulRI(index, int32(scalar.size), true)
	f.a.Load32(tmp, obj, 4)
	end := f.allocReg(maskOf(obj, index, tmp))
	f.pinned = f.pinned.add(end)
	f.a.MovReg64(end, index)
	f.a.AluRI(0, end, int32(gc.PayloadOffset)+int32(scalar.size), true)
	f.a.Cmp64(end, tmp)
	f.trapIf(condA, trapCastFailure)
	f.pinned = f.pinned.remove(end)
	f.pinned = f.pinned.remove(tmp)
	if scalar.typ.isFloat() {
		x := f.materializeF(value)
		f.a.FStoreIdx(obj, index, x, int32(gc.PayloadOffset), scalar.typ == mtF64)
		f.releaseF(x)
	} else {
		r := f.materialize(value)
		f.a.StoreIdx(obj, index, r, int32(gc.PayloadOffset), scalar.size)
		f.release(r)
	}
	f.pinned = f.pinned.remove(index)
	f.release(index)
	done()
	f.spillFloor = oldFloor
	return true
}
