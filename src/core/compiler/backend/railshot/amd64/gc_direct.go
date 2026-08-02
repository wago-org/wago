//go:build amd64

package amd64

import (
	"fmt"
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

func nativeGCCollectorRefStorage(m *wasm.Module, containingType uint32, st wasm.StorageType) bool {
	if st.Packed || st.Val.Kind != wasm.ValRef {
		return false
	}
	heap := st.Val.Ref.Heap
	if heap.Kind == wasm.HeapAbs {
		switch heap.Abs {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
			return true
		default:
			return false
		}
	}
	if heap.Kind == wasm.HeapDefType && heap.Def != nil {
		if int(heap.Def.Index) >= len(heap.Def.Rec.SubTypes) {
			return false
		}
		kind := heap.Def.Rec.SubTypes[heap.Def.Index].Comp.Kind
		return kind == wasm.CompStruct || kind == wasm.CompArray
	}
	if heap.Kind == wasm.HeapTypeIndex {
		target := heap.Type.Index
		if heap.Type.Rec {
			base, _, found := nativeGCRecGroup(m, containingType)
			if !found {
				return false
			}
			target = base + target
		}
		type_, found := nativeGCFlatType(m, target)
		return found && (type_.Comp.Kind == wasm.CompStruct || type_.Comp.Kind == wasm.CompArray)
	}
	return false
}

func nativeGCStructFieldLayout(m *wasm.Module, typeIndex, fieldIndex uint32) (payloadOffset, size uint32, final bool, ok bool) {
	st, found := nativeGCFlatType(m, typeIndex)
	if !found || st.Comp.Kind != wasm.CompStruct || fieldIndex >= uint32(len(st.Comp.Fields)) {
		return 0, 0, false, false
	}
	var off uint32
	for i, field := range st.Comp.Fields {
		align, fieldSize, valid := nativeGCStorageLayout(m, typeIndex, field.Storage)
		if !valid || align == 0 || off > math.MaxUint32-(align-1) {
			return 0, 0, false, false
		}
		off = (off + align - 1) &^ (align - 1)
		if uint32(i) == fieldIndex {
			return off, fieldSize, st.Final, true
		}
		if off > math.MaxUint32-fieldSize {
			return 0, 0, false, false
		}
		off += fieldSize
	}
	return 0, 0, false, false
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

func (f *fn) emitNativeFinalCastArrayLen(typeIndex uint32, nullable bool) error {
	object := f.popValue()
	f.flush() // preserve only values below the consumed reference across the stub
	ref := f.materialize(object)
	if ref != RAX {
		f.a.MovReg64(RAX, ref)
	}
	f.release(ref)
	f.a.MovImm32(RDX, int32(typeIndex))
	if nullable {
		f.a.MovImm32(RCX, 1)
	} else {
		f.a.MovImm32(RCX, 0)
	}
	site := f.a.CallRel32()
	f.sc.gcArrayLenStubSites = append(f.sc.gcArrayLenStubSites, site)
	f.stats.call("gcnative")
	f.pushReg(RAX, mtI32)
	return nil
}

func (f *fn) emitNativeFinalCast(typeIndex uint32, nullable bool) error {
	object := f.popValue()
	f.flush()
	ref := f.materialize(object)
	if ref != RAX {
		f.a.MovReg64(RAX, ref)
	}
	f.release(ref)
	f.a.MovImm32(RDX, int32(typeIndex))
	if nullable {
		f.a.MovImm32(RCX, 1)
	} else {
		f.a.MovImm32(RCX, 0)
	}
	site := f.a.CallRel32()
	f.sc.gcFinalCastStubSites = append(f.sc.gcFinalCastStubSites, site)
	f.stats.call("gcnative")
	result := f.pushReg(RAX, mtI64)
	result.st.gcRoot = true
	return nil
}

func (f *fn) emitNativeFinalCastStructRefGet(typeIndex, fieldOffset uint32, nullable bool) error {
	object := f.popValue()
	f.flush()
	ref := f.materialize(object)
	if ref != RAX {
		f.a.MovReg64(RAX, ref)
	}
	f.release(ref)
	required := uint64(gc.PayloadOffset) + uint64(fieldOffset) + 4
	if required > math.MaxInt32 {
		return fmt.Errorf("amd64: final struct reference field extent %d exceeds native immediate", required)
	}
	f.a.MovImm32(RDX, int32(typeIndex))
	f.a.MovImm32(RCX, int32(required))
	if nullable {
		f.a.MovImm32(RSI, 1)
	} else {
		f.a.MovImm32(RSI, 0)
	}
	site := f.a.CallRel32()
	f.sc.gcStructRefGetStubSites = append(f.sc.gcStructRefGetStubSites, site)
	f.stats.call("gcnative")
	f.a.Load32(RAX, RAX, int32(gc.PayloadOffset+fieldOffset))
	result := f.pushReg(RAX, mtI64)
	result.st.gcRoot = true
	return nil
}

func (f *fn) emitNativeNurseryStructRefSet(typeIndex, fieldIndex, fieldOffset uint32, valueType wasm.ValType) error {
	var savedLocals [16]locState
	if len(f.pinnedLocals) > len(savedLocals) {
		return fmt.Errorf("amd64: %d pinned locals exceed conditional GC store bound", len(f.pinnedLocals))
	}
	for i, local := range f.pinnedLocals {
		savedLocals[i] = f.locals[local].state
	}
	f.flush()
	value := f.s.back()
	object := value.prev
	if value == f.s.head || object == f.s.head || value.kind != ekValue || object.kind != ekValue || value.st.kind != stSlot || object.st.kind != stSlot {
		return fmt.Errorf("amd64: native nursery struct reference store lost canonical operands")
	}
	f.a.Load64(RAX, RSP, f.spillOff(object.st.slot))
	f.a.Load64(RSI, RSP, f.spillOff(value.st.slot))
	required := uint64(gc.PayloadOffset) + uint64(fieldOffset) + 4
	if required > math.MaxInt32 {
		return fmt.Errorf("amd64: final struct reference store extent %d exceeds native immediate", required)
	}
	f.a.MovImm32(RDX, int32(typeIndex))
	f.a.MovImm32(RCX, int32(required))
	site := f.a.CallRel32()
	f.sc.gcStructRefSetStubSites = append(f.sc.gcStructRefSetStubSites, site)
	f.stats.call("gcnative")
	f.a.TestSelf(RAX, false)
	fallback := f.a.JccPlaceholder(condE)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(fallback, f.a.Len())
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
	objectType := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
	err := f.callGCStructHelper(gcStructSet, []wasm.ValType{objectType, valueType, wasm.I32, wasm.I32}, nil)
	f.reloadConditionalGCPinnedLocals(savedLocals[:len(f.pinnedLocals)])
	f.a.PatchRel32(done, f.a.Len())
	return err
}

func (f *fn) emitNativeNurseryArrayRefSet(typeIndex uint32, valueType wasm.ValType) error {
	var savedLocals [16]locState
	if len(f.pinnedLocals) > len(savedLocals) {
		return fmt.Errorf("amd64: %d pinned locals exceed conditional GC store bound", len(f.pinnedLocals))
	}
	for i, local := range f.pinnedLocals {
		savedLocals[i] = f.locals[local].state
	}
	f.flush()
	value := f.s.back()
	index := value.prev
	object := index.prev
	if value == f.s.head || index == f.s.head || object == f.s.head || value.kind != ekValue || index.kind != ekValue || object.kind != ekValue || value.st.kind != stSlot || index.st.kind != stSlot || object.st.kind != stSlot {
		return fmt.Errorf("amd64: native nursery array reference store lost canonical operands")
	}
	f.a.Load64(RAX, RSP, f.spillOff(object.st.slot))
	f.a.Load64(RCX, RSP, f.spillOff(index.st.slot))
	f.a.Load64(RSI, RSP, f.spillOff(value.st.slot))
	f.a.MovImm32(RDX, int32(typeIndex))
	site := f.a.CallRel32()
	f.sc.gcArrayRefSetStubSites = append(f.sc.gcArrayRefSetStubSites, site)
	f.stats.call("gcnative")
	f.a.TestSelf(RAX, false)
	fallback := f.a.JccPlaceholder(condE)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(fallback, f.a.Len())
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	objectType := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
	err := f.callGCStructHelper(gcArraySet, []wasm.ValType{objectType, wasm.I32, valueType, wasm.I32}, nil)
	f.reloadConditionalGCPinnedLocals(savedLocals[:len(f.pinnedLocals)])
	f.a.PatchRel32(done, f.a.Len())
	return err
}

// reloadConditionalGCPinnedLocals emits only on the cold helper fallback. The
// fast native edge preserved the registers but skipped the helper's spill code;
// restoring the pre-branch state makes both edges agree without hot-path stores.
func (f *fn) reloadConditionalGCPinnedLocals(saved []locState) {
	for i, local := range f.pinnedLocals {
		state := saved[i]
		if state == lsReg || state == lsStackReg {
			f.loadLocalReg(local, f.locals[local].reg, f.locals[local].isFloat)
		}
		f.locals[local].state = state
	}
}

func (f *fn) emitNativeFinalArrayRefGet(typeIndex uint32) error {
	f.flush()
	indexValue := f.popValue()
	object := f.popValue()
	ref := f.materialize(object)
	if ref != RAX {
		f.a.MovReg64(RAX, ref)
	}
	f.release(ref)
	f.pinned = f.pinned.add(RAX)
	index := f.materialize(indexValue)
	if index != RCX {
		f.a.MovReg64(RCX, index)
	}
	f.release(index)
	f.pinned = f.pinned.remove(RAX)
	f.a.MovImm32(RDX, int32(typeIndex))
	site := f.a.CallRel32()
	f.sc.gcArrayRefGetSites = append(f.sc.gcArrayRefGetSites, site)
	f.stats.call("gcnative")
	result := f.pushReg(RAX, mtI64)
	result.st.gcRoot = true
	return nil
}

func (f *fn) emitNativeGCStubs() {
	if len(f.sc.gcArrayLenStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeFinalCastArrayLenStub()
		for _, site := range f.sc.gcArrayLenStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
	if len(f.sc.gcFinalCastStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeFinalCastStub()
		for _, site := range f.sc.gcFinalCastStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
	if len(f.sc.gcArrayRefGetSites) != 0 {
		stub := f.a.Len()
		f.emitNativeFinalArrayRefGetStub()
		for _, site := range f.sc.gcArrayRefGetSites {
			f.a.PatchRel32(site, stub)
		}
	}
	if len(f.sc.gcStructRefGetStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeFinalCastStructRefResolverStub()
		for _, site := range f.sc.gcStructRefGetStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
	if len(f.sc.gcStructRefSetStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeNurseryStructRefSetStub()
		for _, site := range f.sc.gcStructRefSetStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
	if len(f.sc.gcArrayRefSetStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeNurseryArrayRefSetStub()
		for _, site := range f.sc.gcArrayRefSetStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
}

// emitNativeFinalCastArrayLenStub emits one per-function checked native stub.
// Inputs are EAX=compact reference, EDX=module-local final array type, and
// ECX=cast-null flag. The result is returned in EAX. Only caller-saved registers
// are clobbered; callers home pinned caller-saved locals before entering.
func (f *fn) emitNativeFinalCastArrayLenStub() {
	a := f.a
	// R9-R11 may hold extended pinned locals or value-pinned globals. Preserve
	// only the registers this function actually reserves.
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, reg := range f.globalReg {
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	a.TestSelf(RAX, false)
	nonNull := a.JccPlaceholder(condNE)
	a.TestSelf(RCX, false)
	f.trapIf(condE, trapCastFailure)
	f.trapAlways(trapNullReference)
	a.PatchRel32(nonNull, a.Len())

	// i31 immediates cannot satisfy a defined array cast.
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	f.trapIf(condNE, trapCastFailure)

	// Resolve the module-local type to immutable Runtime-domain identity.
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewLocalTypeCountOffset)
	a.Cmp32(RDX, R10)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	// Resolve and validate the compact handle against collector ABI v1.
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewHandleStrideOffset)
	a.AluRI(cmpDigit, R10, gc.NativeHandleStride, false)
	f.trapIf(condNE, trapCastFailure)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)

	// Select the current backing space and validate the handle range.
	a.Load32(R10, R9, 16)
	a.ShiftImm(5, R10, 16, false)
	a.AluRI(4, R10, 0xff, false)
	a.TestSelf(R10, false)
	f.trapIf(condE, trapCastFailure)
	a.AluRI(cmpDigit, R10, gc.NativeSpaceCount-1, false)
	f.trapIf(condA, trapCastFailure)
	a.ImulRI(R10, gc.NativeViewSpaceStride, true)
	a.LeaDisp(R8, R8, gc.NativeViewSpacesOffset)
	a.Add64(R8, R10)
	a.Load64(R11, R8, gc.NativeSpaceBaseOffset)
	a.Load32(R8, R8, gc.NativeSpaceBytesOffset)
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, R8)
	f.trapIf(condA, trapCastFailure)
	a.Sub32(R8, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, R8)
	f.trapIf(condA, trapCastFailure)
	a.AluRI(cmpDigit, RCX, int32(gc.HeaderSize), false)
	f.trapIf(condB, trapCastFailure)

	// Exact final type equality proves the object is the statically known array.
	a.Add64(R11, RAX)
	a.Load32(RCX, R11, 0)
	a.Cmp32(RCX, RDX)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(RAX, R11, 8)
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Ret()
}

// emitNativeFinalCastStub validates one final defined collector cast and returns
// the original compact reference. Null succeeds only for ref.cast_null.
func (f *fn) emitNativeFinalCastStub() {
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, reg := range f.globalReg {
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	a.TestSelf(RAX, false)
	nonNull := a.JccPlaceholder(condNE)
	a.TestSelf(RCX, false)
	f.trapIf(condE, trapCastFailure)
	nullSuccess := a.JmpPlaceholder()
	a.PatchRel32(nonNull, a.Len())

	// Save the physical compact reference while EAX becomes the handle index.
	a.MovRegReg32(RSI, RAX)
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	f.trapIf(condNE, trapCastFailure)

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewLocalTypeCountOffset)
	a.Cmp32(RDX, R10)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewHandleStrideOffset)
	a.AluRI(cmpDigit, R10, gc.NativeHandleStride, false)
	f.trapIf(condNE, trapCastFailure)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)

	a.Load32(R10, R9, 16)
	a.ShiftImm(5, R10, 16, false)
	a.AluRI(4, R10, 0xff, false)
	a.TestSelf(R10, false)
	f.trapIf(condE, trapCastFailure)
	a.AluRI(cmpDigit, R10, gc.NativeSpaceCount-1, false)
	f.trapIf(condA, trapCastFailure)
	a.ImulRI(R10, gc.NativeViewSpaceStride, true)
	a.LeaDisp(R8, R8, gc.NativeViewSpacesOffset)
	a.Add64(R8, R10)
	a.Load64(R11, R8, gc.NativeSpaceBaseOffset)
	a.Load32(R8, R8, gc.NativeSpaceBytesOffset)
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, R8)
	f.trapIf(condA, trapCastFailure)
	a.Sub32(R8, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, R8)
	f.trapIf(condA, trapCastFailure)
	a.AluRI(cmpDigit, RCX, int32(gc.HeaderSize), false)
	f.trapIf(condB, trapCastFailure)

	a.Add64(R11, RAX)
	a.Load32(RCX, R11, 0)
	a.Cmp32(RCX, RDX)
	f.trapIf(condNE, trapCastFailure)
	a.MovRegReg32(RAX, RSI)
	done := a.Len()
	a.PatchRel32(nullSuccess, done)
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Ret()
}

// emitNativeFinalArrayRefGetStub loads one collector reference from a final
// typed array. EAX is the compact object, EDX the local type, and ECX the index.
func (f *fn) emitNativeFinalArrayRefGetStub() {
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, reg := range f.globalReg {
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	a.TestSelf(RAX, false)
	f.trapIf(condE, trapNullReference)
	a.MovRegReg32(RSI, RCX)
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	f.trapIf(condNE, trapCastFailure)

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewLocalTypeCountOffset)
	a.Cmp32(RDX, R10)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewHandleStrideOffset)
	a.AluRI(cmpDigit, R10, gc.NativeHandleStride, false)
	f.trapIf(condNE, trapCastFailure)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)

	a.Load32(R10, R9, 16)
	a.ShiftImm(5, R10, 16, false)
	a.AluRI(4, R10, 0xff, false)
	a.TestSelf(R10, false)
	f.trapIf(condE, trapCastFailure)
	a.AluRI(cmpDigit, R10, gc.NativeSpaceCount-1, false)
	f.trapIf(condA, trapCastFailure)
	a.ImulRI(R10, gc.NativeViewSpaceStride, true)
	a.LeaDisp(R8, R8, gc.NativeViewSpacesOffset)
	a.Add64(R8, R10)
	a.Load64(R11, R8, gc.NativeSpaceBaseOffset)
	a.Load32(R8, R8, gc.NativeSpaceBytesOffset)
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, R8)
	f.trapIf(condA, trapCastFailure)
	a.Sub32(R8, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, R8)
	f.trapIf(condA, trapCastFailure)
	a.AluRI(cmpDigit, RCX, int32(gc.PayloadOffset), false)
	f.trapIf(condB, trapCastFailure)

	a.Add64(R11, RAX)
	a.Load32(RCX, R11, 0)
	a.Cmp32(RCX, RDX)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R11, 8)
	a.Cmp32(RSI, R10)
	f.trapIf(condAE, trapBuiltin)
	a.ImulRI(RSI, 4, true)
	a.Load32(R10, R11, 4)
	a.MovReg64(R8, RSI)
	a.AluRI(0, R8, int32(gc.PayloadOffset)+4, true)
	a.Load32(RDX, R9, gc.NativeHandleSizeOffset)
	a.Cmp64(R8, RDX)
	f.trapIf(condA, trapCastFailure)
	a.Cmp64(R8, R10)
	f.trapIf(condA, trapCastFailure)
	a.LoadIdx(RAX, R11, RSI, int32(gc.PayloadOffset), 4, false, false)
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Ret()
}

// emitNativeFinalCastStructRefResolverStub validates a final cast and returns
// the resolved object-header pointer. Inputs are EAX=compact reference,
// EDX=module-local final struct type, ECX=minimum object extent, and
// ESI=cast-null flag. The call site immediately performs one constant-offset
// reference load before any safepoint can relocate the object.
func (f *fn) emitNativeFinalCastStructRefResolverStub() {
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, reg := range f.globalReg {
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	a.MovRegReg32(RDI, RCX) // preserve required extent
	a.TestSelf(RAX, false)
	nonNull := a.JccPlaceholder(condNE)
	a.TestSelf(RSI, false)
	f.trapIf(condE, trapCastFailure)
	f.trapAlways(trapNullReference)
	a.PatchRel32(nonNull, a.Len())

	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	f.trapIf(condNE, trapCastFailure)

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewLocalTypeCountOffset)
	a.Cmp32(RDX, R10)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewHandleStrideOffset)
	a.AluRI(cmpDigit, R10, gc.NativeHandleStride, false)
	f.trapIf(condNE, trapCastFailure)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)

	a.Load32(R10, R9, 16)
	a.ShiftImm(5, R10, 16, false)
	a.AluRI(4, R10, 0xff, false)
	a.TestSelf(R10, false)
	f.trapIf(condE, trapCastFailure)
	a.AluRI(cmpDigit, R10, gc.NativeSpaceCount-1, false)
	f.trapIf(condA, trapCastFailure)
	a.ImulRI(R10, gc.NativeViewSpaceStride, true)
	a.LeaDisp(R8, R8, gc.NativeViewSpacesOffset)
	a.Add64(R8, R10)
	a.Load64(R11, R8, gc.NativeSpaceBaseOffset)
	a.Load32(R8, R8, gc.NativeSpaceBytesOffset)
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, R8)
	f.trapIf(condA, trapCastFailure)
	a.Sub32(R8, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, R8)
	f.trapIf(condA, trapCastFailure)
	a.Cmp32(RCX, RDI)
	f.trapIf(condB, trapCastFailure)

	a.Add64(R11, RAX)
	a.Load32(RCX, R11, 0)
	a.Cmp32(RCX, RDX)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(RCX, R11, 4)
	a.Cmp32(RCX, RDI)
	f.trapIf(condB, trapCastFailure)
	a.MovReg64(RSI, R11)
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.MovReg64(RAX, RSI)
	a.Ret()
}

// emitNativeNurseryStructRefSetStub resolves an exact final struct only when the
// parent currently lives in Throughput nursery space. Nursery stores need no
// remembered-set or Tiny incremental barrier. The stub performs the checked
// store and returns EAX=1; EAX=0 selects the exact Go helper fallback.
func (f *fn) emitNativeNurseryStructRefSetStub() {
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, reg := range f.globalReg {
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	a.MovRegReg32(RDI, RSI) // compact child
	a.MovRegReg32(RSI, RCX) // required parent extent
	var fallback [10]int
	nfallback := 0
	addFallback := func(site int) { fallback[nfallback], nfallback = site, nfallback+1 }

	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	addFallback(a.JccPlaceholder(condNE))

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewLocalTypeCountOffset)
	a.Cmp32(RDX, R10)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewHandleStrideOffset)
	a.AluRI(cmpDigit, R10, gc.NativeHandleStride, false)
	f.trapIf(condNE, trapCastFailure)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	addFallback(a.JccPlaceholder(condAE))
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)
	a.Load32(R10, R9, 16)
	a.ShiftImm(5, R10, 16, false)
	a.AluRI(4, R10, 0xff, false)
	a.AluRI(cmpDigit, R10, int32(gc.NativeSpaceNursery), false)
	addFallback(a.JccPlaceholder(condNE))

	a.Load64(R11, R8, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBaseOffset))
	a.Load32(R10, R8, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBytesOffset))
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, R10)
	addFallback(a.JccPlaceholder(condA))
	a.Sub32(R10, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, R10)
	addFallback(a.JccPlaceholder(condA))
	a.Cmp32(RCX, RSI)
	addFallback(a.JccPlaceholder(condB))
	a.Add64(R11, RAX)
	a.Load32(RCX, R11, 0)
	a.Cmp32(RCX, RDX)
	addFallback(a.JccPlaceholder(condNE))
	a.Load32(RCX, R11, 4)
	a.Cmp32(RCX, RSI)
	f.trapIf(condB, trapCastFailure)

	// Null and i31 children are valid anyref values. Object children must name a
	// live handle in this exact collector before the direct store is admitted.
	a.TestSelf(RDI, false)
	childValid := a.JccPlaceholder(condE)
	a.MovRegReg32(RAX, RDI)
	a.AluRI(4, RAX, 1, false)
	a.TestSelf(RAX, false)
	childI31 := a.JccPlaceholder(condNE)
	a.MovRegReg32(RAX, RDI)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	addFallback(a.JccPlaceholder(condAE))
	a.Load64(R10, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R10, RAX)
	a.Load32(RAX, R10, 16)
	a.ShiftImm(5, RAX, 16, false)
	a.AluRI(4, RAX, 0xff, false)
	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.PatchRel32(childValid, a.Len())
	a.PatchRel32(childI31, a.Len())
	a.Add64(R11, RSI)
	a.Store32(R11, -4, RDI)
	a.MovImm32(RCX, 1)
	done := a.JmpPlaceholder()

	fallbackAt := a.Len()
	for i := 0; i < nfallback; i++ {
		a.PatchRel32(fallback[i], fallbackAt)
	}
	a.MovImm32(RCX, 0)
	a.PatchRel32(done, a.Len())
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.MovReg64(RAX, RCX)
	a.Ret()
}

func (f *fn) emitNativeNurseryArrayRefSetStub() {
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, reg := range f.globalReg {
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	a.MovRegReg32(RDI, RSI) // compact child
	a.MovRegReg32(RSI, RCX) // array index
	var fallback [12]int
	nfallback := 0
	addFallback := func(site int) { fallback[nfallback], nfallback = site, nfallback+1 }

	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	addFallback(a.JccPlaceholder(condNE))

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeInstanceViewLocalTypeCountOffset)
	a.Cmp32(RDX, R10)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.TestSelf(R8, true)
	f.trapIf(condE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewVersionOffset)
	a.AluRI(cmpDigit, R10, int32(gc.NativeABIVersion), false)
	f.trapIf(condNE, trapCastFailure)
	a.Load32(R10, R8, gc.NativeViewHandleStrideOffset)
	a.AluRI(cmpDigit, R10, gc.NativeHandleStride, false)
	f.trapIf(condNE, trapCastFailure)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	addFallback(a.JccPlaceholder(condAE))
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)
	a.Load32(R10, R9, 16)
	a.ShiftImm(5, R10, 16, false)
	a.AluRI(4, R10, 0xff, false)
	a.AluRI(cmpDigit, R10, int32(gc.NativeSpaceNursery), false)
	addFallback(a.JccPlaceholder(condNE))

	a.Load64(R11, R8, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBaseOffset))
	a.Load32(R10, R8, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBytesOffset))
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, R10)
	addFallback(a.JccPlaceholder(condA))
	a.Sub32(R10, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, R10)
	addFallback(a.JccPlaceholder(condA))
	a.AluRI(cmpDigit, RCX, int32(gc.PayloadOffset), false)
	addFallback(a.JccPlaceholder(condB))
	a.Add64(R11, RAX)
	a.Load32(R10, R11, 0)
	a.Cmp32(R10, RDX)
	addFallback(a.JccPlaceholder(condNE))
	a.Load32(R10, R11, 8)
	a.Cmp32(RSI, R10)
	f.trapIf(condAE, trapBuiltin)
	a.ImulRI(RSI, 4, true)
	a.MovReg64(R10, RSI)
	a.AluRI(0, R10, int32(gc.PayloadOffset)+4, true)
	a.Cmp64(R10, RCX)
	f.trapIf(condA, trapCastFailure)
	a.Load32(RCX, R11, 4)
	a.Cmp64(R10, RCX)
	f.trapIf(condA, trapCastFailure)

	a.TestSelf(RDI, false)
	childValid := a.JccPlaceholder(condE)
	a.MovRegReg32(RAX, RDI)
	a.AluRI(4, RAX, 1, false)
	a.TestSelf(RAX, false)
	childI31 := a.JccPlaceholder(condNE)
	a.MovRegReg32(RAX, RDI)
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	addFallback(a.JccPlaceholder(condAE))
	a.Load64(R10, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R10, RAX)
	a.Load32(RAX, R10, 16)
	a.ShiftImm(5, RAX, 16, false)
	a.AluRI(4, RAX, 0xff, false)
	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.PatchRel32(childValid, a.Len())
	a.PatchRel32(childI31, a.Len())
	a.StoreIdx(R11, RSI, RDI, int32(gc.PayloadOffset), 4)
	a.MovImm32(RAX, 1)
	done := a.JmpPlaceholder()

	fallbackAt := a.Len()
	for i := 0; i < nfallback; i++ {
		a.PatchRel32(fallback[i], fallbackAt)
	}
	a.MovImm32(RAX, 0)
	a.PatchRel32(done, a.Len())
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Ret()
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
