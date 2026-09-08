//go:build amd64

package amd64

import (
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// directGCScalar describes a pointer-free scalar storage location that can be
// accessed without entering the parked Go helper. Reference and v128 storage
// deliberately remain helper-bound until their barrier/vector paths are proven.
type gcSharedStubKind uint8

const (
	gcSharedStubNone gcSharedStubKind = iota
	gcSharedStubResolveObject
	gcSharedStubMax
)

type directGCScalar struct {
	size int
	typ  machineType
}

type nativeGCStructAllocField struct {
	offset   uint32
	size     uint32
	slot     uint32
	ref      bool
	nullable bool
}

type nativeGCArrayAllocLayout struct {
	elemSize    uint32
	elemAlign   uint32
	valueSlots  uint32
	ref         bool
	nullable    bool
	pointerFree bool
}

func nativeGCArrayLayout(enabled bool, m *wasm.Module, typeIndex uint32) (nativeGCArrayAllocLayout, bool) {
	if !enabled {
		return nativeGCArrayAllocLayout{}, false
	}
	st, found := nativeGCFlatType(m, typeIndex)
	if !found || !st.Final || st.Comp.Kind != wasm.CompArray {
		return nativeGCArrayAllocLayout{}, false
	}
	field := st.Comp.Array
	align, size, ok := nativeGCStorageLayout(m, typeIndex, field.Storage())
	if !ok || align == 0 || size == 0 {
		return nativeGCArrayAllocLayout{}, false
	}
	layout := nativeGCArrayAllocLayout{elemSize: size, elemAlign: align, valueSlots: 1, pointerFree: true}
	if size == 16 {
		layout.valueSlots = 2
	}
	if field.Storage().Val().Kind() == wasm.ValRef {
		heap := field.Storage().Val().Ref().Heap()
		if field.Storage().Packed() || heap.Kind() != wasm.HeapAbs || (heap.Abs() != wasm.HeapAny && heap.Abs() != wasm.HeapEq) || size != 4 {
			return nativeGCArrayAllocLayout{}, false
		}
		layout.ref = true
		layout.nullable = field.Storage().Val().Ref().Nullable()
		layout.pointerFree = false
	} else if field.Storage().Val().Kind() != wasm.ValNum && field.Storage().Val().Kind() != wasm.ValVec {
		return nativeGCArrayAllocLayout{}, false
	}
	return layout, true
}

func nativeGCStructAllocLayout(m *wasm.Module, typeIndex uint32) (fields []nativeGCStructAllocField, objectSize, objectAlign uint32, pointerFree bool, ok bool) {
	st, found := nativeGCFlatType(m, typeIndex)
	if !found || !st.Final || st.Comp.Kind != wasm.CompStruct {
		return nil, 0, 0, false, false
	}
	fields = make([]nativeGCStructAllocField, 0, len(st.Comp.Fields))
	var off, slot uint32
	maxAlign := uint32(1)
	pointerFree = true
	for _, field := range st.Comp.Fields {
		align, size, valid := nativeGCStorageLayout(m, typeIndex, field.Storage())
		if !valid || field.Storage().Packed() || align == 0 || off > math.MaxUint32-(align-1) {
			return nil, 0, 0, false, false
		}
		off = (off + align - 1) &^ (align - 1)
		entry := nativeGCStructAllocField{offset: off, size: size, slot: slot}
		if field.Storage().Val().Kind() == wasm.ValRef {
			heap := field.Storage().Val().Ref().Heap()
			if heap.Kind() != wasm.HeapAbs || (heap.Abs() != wasm.HeapAny && heap.Abs() != wasm.HeapEq) || size != 4 {
				return nil, 0, 0, false, false
			}
			entry.ref = true
			entry.nullable = field.Storage().Val().Ref().Nullable()
			pointerFree = false
		} else if field.Storage().Val().Kind() != wasm.ValNum && field.Storage().Val().Kind() != wasm.ValVec {
			return nil, 0, 0, false, false
		}
		fields = append(fields, entry)
		if off > math.MaxUint32-size {
			return nil, 0, 0, false, false
		}
		off += size
		if align > maxAlign {
			maxAlign = align
		}
		slot++
		if size == 16 {
			slot++
		}
		if slot+1 > 64 {
			return nil, 0, 0, false, false
		}
	}
	if off > math.MaxUint32-(maxAlign-1) {
		return nil, 0, 0, false, false
	}
	payload := (off + maxAlign - 1) &^ (maxAlign - 1)
	if payload > math.MaxUint32-gc.PayloadOffset-7 {
		return nil, 0, 0, false, false
	}
	objectSize = (payload + gc.PayloadOffset + 7) &^ 7
	if objectSize > gc.NativeAllocationChunkBytes {
		return nil, 0, 0, false, false
	}
	objectAlign = maxAlign
	if objectAlign < 8 {
		objectAlign = 8
	}
	return fields, objectSize, objectAlign, pointerFree, true
}

func directGCScalarStorage(st wasm.StorageType) (directGCScalar, bool) {
	if st.Packed() {
		switch st.Pack() {
		case wasm.PackI8:
			return directGCScalar{size: 1, typ: mtI32}, true
		case wasm.PackI16:
			return directGCScalar{size: 2, typ: mtI32}, true
		default:
			return directGCScalar{}, false
		}
	}
	if st.Val().Kind() != wasm.ValNum {
		return directGCScalar{}, false
	}
	switch st.Val().Num() {
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
	if st.Packed() {
		switch st.Pack() {
		case wasm.PackI8:
			return 1, 1, true
		case wasm.PackI16:
			return 2, 2, true
		default:
			return 0, 0, false
		}
	}
	switch st.Val().Kind() {
	case wasm.ValNum:
		switch st.Val().Num() {
		case wasm.NumI32, wasm.NumF32:
			return 4, 4, true
		case wasm.NumI64, wasm.NumF64:
			return 8, 8, true
		}
	case wasm.ValVec:
		return 16, 16, true
	case wasm.ValRef:
		h := st.Val().Ref().Heap()
		if h.Kind() == wasm.HeapAbs {
			switch h.Abs() {
			case wasm.HeapFunc, wasm.HeapNoFunc, wasm.HeapExtern, wasm.HeapNoExtern:
				return 8, 8, true
			default:
				return 4, 4, true
			}
		}
		if h.Kind() == wasm.HeapDefType {
			kind, valid := h.DefCompKind()
			if valid && kind == wasm.CompFunc {
				return 8, 8, true
			}
			return 4, 4, true
		}
		if h.Kind() == wasm.HeapTypeIndex {
			target := h.Type().Index
			if h.Type().Rec {
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
	if st.Packed() || st.Val().Kind() != wasm.ValRef {
		return false
	}
	heap := st.Val().Ref().Heap()
	if heap.Kind() == wasm.HeapAbs {
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
			return true
		default:
			return false
		}
	}
	if heap.Kind() == wasm.HeapDefType {
		kind, valid := heap.DefCompKind()
		if !valid {
			return false
		}
		return kind == wasm.CompStruct || kind == wasm.CompArray
	}
	if heap.Kind() == wasm.HeapTypeIndex {
		target := heap.Type().Index
		if heap.Type().Rec {
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
		align, fieldSize, valid := nativeGCStorageLayout(m, typeIndex, field.Storage())
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
	scalar, ok = directGCScalarStorage(st.Comp.Fields[fieldIndex].Storage())
	if !ok {
		return 0, directGCScalar{}, false, false
	}
	var off uint32
	for i, field := range st.Comp.Fields {
		align, size, valid := nativeGCStorageLayout(m, typeIndex, field.Storage())
		if !valid || align == 0 || off > math.MaxUint32-(align-1) {
			return 0, directGCScalar{}, false, false
		}
		off = (off + align - 1) &^ (align - 1)
		if uint32(i) == fieldIndex {
			if uint64(gc.PayloadOffset)+uint64(off)+uint64(scalar.size) > math.MaxInt32 {
				return 0, directGCScalar{}, false, false
			}
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
	scalar, ok := directGCScalarStorage(st.Comp.Array.Storage())
	return scalar, st.Final, ok
}

// buildModuleGCSharedStubs emits one compact module-owned island for every
// referenced noncollecting GC leaf family. Offsets are relative to the returned
// byte slice; callers add the island's module base before patching CALL rel32.
func buildModuleGCSharedStubs(relocs [][]callReloc) ([]byte, [gcSharedStubMax]int) {
	var used [gcSharedStubMax]bool
	for i := range relocs {
		for _, reloc := range relocs[i] {
			if reloc.gcStub > gcSharedStubNone && reloc.gcStub < gcSharedStubMax {
				used[reloc.gcStub] = true
			}
		}
	}
	var offsets [gcSharedStubMax]int
	for i := range offsets {
		offsets[i] = -1
	}
	if !used[gcSharedStubResolveObject] {
		return nil, offsets
	}
	a := &x64.Asm{}
	offsets[gcSharedStubResolveObject] = a.Len()
	emitModuleGCResolveObjectStub(a)
	return a.B, offsets
}

// emitModuleGCResolveObjectStub implements the AMD64 noncollecting GC leaf ABI:
//
//	EAX compact object ref, EDX static module-local type, ECX required bytes
//	RAX resolved object header on success, EDX zero on success or trap code
//	clobbers RAX,RCX,RDX,RDI,R8 and flags; preserves R9-R11 explicitly
//	no Go transition, allocation, collection, root publication, or safepoint
//
// The compact reference remains in the caller's canonical local/stack root. A
// returned raw address is valid only until the caller's next invalidating edge.
func emitModuleGCResolveObjectStub(a *x64.Asm) {
	a.Push(R9)
	a.Push(R10)
	a.Push(R11)
	a.MovRegReg32(RDI, RCX) // required object extent

	a.TestSelf(RAX, false)
	nullFailure := a.JccPlaceholder(condE)
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	castFailures := []int{a.JccPlaceholder(condNE)}

	// Immutable view/type-map shape was proved at artifact load + instantiation.
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)

	// Dynamic semantic validation remains exact: handle range/liveness, space,
	// backing extent, object extent, and canonical runtime type.
	a.ShiftImm(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	castFailures = append(castFailures, a.JccPlaceholder(condAE))
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)

	a.Load32(R10, R9, 16)
	a.ShiftImm(5, R10, 16, false)
	a.AluRI(4, R10, 0xff, false)
	a.TestSelf(R10, false)
	castFailures = append(castFailures, a.JccPlaceholder(condE))
	a.AluRI(cmpDigit, R10, gc.NativeSpaceCount-1, false)
	castFailures = append(castFailures, a.JccPlaceholder(condA))
	a.ImulRI(R10, gc.NativeViewSpaceStride, true)
	a.LeaDisp(R8, R8, gc.NativeViewSpacesOffset)
	a.Add64(R8, R10)
	a.Load64(R11, R8, gc.NativeSpaceBaseOffset)
	a.Load32(R8, R8, gc.NativeSpaceBytesOffset)
	a.Load32(RAX, R9, gc.NativeHandleOffsetOffset)
	a.Cmp32(RAX, R8)
	castFailures = append(castFailures, a.JccPlaceholder(condA))
	a.Sub32(R8, RAX)
	a.Load32(RCX, R9, gc.NativeHandleSizeOffset)
	a.Cmp32(RCX, R8)
	castFailures = append(castFailures, a.JccPlaceholder(condA))
	a.Cmp32(RCX, RDI)
	castFailures = append(castFailures, a.JccPlaceholder(condB))

	a.Add64(R11, RAX)
	a.Load32(RCX, R11, 0)
	a.Cmp32(RCX, RDX)
	castFailures = append(castFailures, a.JccPlaceholder(condNE))
	a.Load32(RCX, R11, 4)
	a.Cmp32(RCX, RDI)
	castFailures = append(castFailures, a.JccPlaceholder(condB))
	a.MovReg64(RAX, R11)
	a.XorSelf32(RDX)
	done := a.JmpPlaceholder()

	nullAt := a.Len()
	a.MovImm32(RDX, int32(trapNullReference))
	a.XorSelf32(RAX)
	nullDone := a.JmpPlaceholder()

	castAt := a.Len()
	a.MovImm32(RDX, int32(trapCastFailure))
	a.XorSelf32(RAX)
	for _, branch := range castFailures {
		a.PatchRel32(branch, castAt)
	}
	a.PatchRel32(nullFailure, nullAt)
	finish := a.Len()
	a.PatchRel32(done, finish)
	a.PatchRel32(nullDone, finish)
	a.Pop(R11)
	a.Pop(R10)
	a.Pop(R9)
	a.Ret()
}

func (f *fn) emitDirectGCObject(object *elem, localType, requiredBytes uint32, local int, hasLocal bool) (obj Reg, done func()) {
	if gcResolveReuseEnabled && hasLocal && f.gcResolved.valid &&
		f.gcResolved.local == local && f.gcResolved.typeIndex == localType &&
		requiredBytes <= f.gcResolved.requiredBytes {
		// Consume the repeated compact local.get while retaining the compact local
		// itself as the exact root. No raw address is reconstructed.
		ref := f.materialize(object)
		f.release(ref)
		f.stats.addGCHandleResolutionReuse()
		f.stats.peep("gc-resolve-reuse")
		return f.gcResolved.reg, func() {}
	}
	f.invalidateGCResolvedObject()
	if f.gcDeferResolver && f.gcHandleResolutions != 0 {
		f.gcSharedResolver = true
		f.gcDeferResolver = false
	}
	if f.gcSharedResolver {
		obj, done = f.emitSharedCheckedGCObject(object, localType, requiredBytes)
	} else {
		obj, done = f.emitCheckedGCObject(object, localType, requiredBytes)
	}
	f.gcHandleResolutions++
	f.stats.addGCHandleResolution()
	if !gcResolveReuseEnabled || !hasLocal {
		return obj, done
	}
	cacheReg := f.gcResolvedRegister()
	if cacheReg == regNone {
		return obj, done
	}
	f.a.MovReg64(cacheReg, obj)
	done()
	f.pinned = f.pinned.add(cacheReg)
	f.gcResolved = gcResolvedObject{valid: true, local: local, typeIndex: localType, requiredBytes: requiredBytes, reg: cacheReg}
	return cacheReg, func() {}
}

func (f *fn) emitSharedCheckedGCObject(object *elem, localType, requiredBytes uint32) (obj Reg, done func()) {
	ref := f.materialize(object)
	if ref != RAX {
		f.a.MovReg64(RAX, ref)
	}
	f.release(ref)
	f.a.MovImm32(RDX, int32(localType))
	f.a.MovImm32(RCX, int32(requiredBytes))
	site := f.a.CallRel32()
	f.relocs = append(f.relocs, f.newGCStubCallReloc(site, gcSharedStubResolveObject))
	f.stats.call("gcnative-leaf")
	f.stats.peep("gc-shared-resolve-call")

	f.a.TestSelf(RDX, false)
	success := f.a.JccPlaceholder(condE)
	f.a.AluRI(cmpDigit, RDX, int32(trapNullReference), false)
	f.trapIf(condE, trapNullReference)
	f.trapAlways(trapCastFailure)
	f.a.PatchRel32(success, f.a.Len())
	f.pinned = f.pinned.add(RAX)
	return RAX, func() { f.pinned = f.pinned.remove(RAX) }
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

	// Instantiation has already proved the immutable instance/collector ABI and
	// exact module-local type-map shape. Reload only the immutable map pointer and
	// the mutable collector view; semantic handle/object checks remain below.
	f.a.Load64(view, RBX, -int32(abi.GCNativeViewPtrOffset))
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

func (f *fn) emitNativeDefinedCast(typeIndex uint32, nullable, exact bool) error {
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
	if exact {
		f.a.MovImm32(RSI, 1)
	} else {
		f.a.MovImm32(RSI, 0)
	}
	site := f.a.CallRel32()
	f.sc.gcFinalCastStubSites = append(f.sc.gcFinalCastStubSites, site)
	f.stats.call("gcnative")
	result := f.pushReg(RAX, mtI64)
	result.st.setGCRoot(true)
	return nil
}

func (f *fn) emitNativeDefinedTest(typeIndex uint32, nullable bool) error {
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
	f.a.MovImm32(RSI, 0) // ref.test does not admit exact heap markers
	site := f.a.CallRel32()
	f.sc.gcDefinedTestStubSites = append(f.sc.gcDefinedTestStubSites, site)
	f.stats.call("gcnative")
	f.pushReg(RAX, mtI32)
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
	result.st.setGCRoot(true)
	return nil
}

func (f *fn) emitNativeBarrierSafeStructRefSet(typeIndex, fieldIndex, fieldOffset uint32, valueType wasm.ValType) error {
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
	if value == f.s.head || object == f.s.head || !value.isValue() || !object.isValue() || value.st.kind != stSlot || object.st.kind != stSlot {
		return fmt.Errorf("amd64: native nursery struct reference store lost canonical operands")
	}
	f.a.Load64(RAX, RSP, f.spillOff(object.st.slotIndex()))
	f.a.Load64(RSI, RSP, f.spillOff(value.st.slotIndex()))
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

func (f *fn) emitNativeCardSafeArrayRefSet(typeIndex uint32, valueType wasm.ValType) error {
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
	if value == f.s.head || index == f.s.head || object == f.s.head || !value.isValue() || !index.isValue() || !object.isValue() || value.st.kind != stSlot || index.st.kind != stSlot || object.st.kind != stSlot {
		return fmt.Errorf("amd64: native nursery array reference store lost canonical operands")
	}
	f.a.Load64(RAX, RSP, f.spillOff(object.st.slotIndex()))
	f.a.Load64(RCX, RSP, f.spillOff(index.st.slotIndex()))
	f.a.Load64(RSI, RSP, f.spillOff(value.st.slotIndex()))
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
	result.st.setGCRoot(true)
	return nil
}

func (f *fn) emitNativeGCStubs() {
	before := f.a.Len()
	defer func() {
		n := f.a.Len() - before
		f.stats.addGCSharedStubBytes(n)
		f.stats.addGCHandleResolutionBytes(n)
	}()
	if len(f.sc.gcArrayAllocStubSites) != 0 {
		type arrayStubKey struct {
			typeIndex uint32
			count     uint32
			mode      gcArrayAllocMode
		}
		stubs := make(map[arrayStubKey]int)
		for _, site := range f.sc.gcArrayAllocStubSites {
			key := arrayStubKey{typeIndex: site.typeIndex, count: site.count, mode: site.mode}
			stub, found := stubs[key]
			if !found {
				stub = f.a.Len()
				f.emitNativeArrayAllocStub(site)
				stubs[key] = stub
			}
			f.a.PatchRel32(site.site, stub)
		}
	}
	if len(f.sc.gcStructAllocStubSites) != 0 {
		stubs := make(map[uint32]int)
		for _, site := range f.sc.gcStructAllocStubSites {
			stub, found := stubs[site.typeIndex]
			if !found {
				stub = f.a.Len()
				f.emitNativeStructAllocStub(site.typeIndex)
				stubs[site.typeIndex] = stub
			}
			f.a.PatchRel32(site.site, stub)
		}
	}
	if len(f.sc.gcArrayLenStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeFinalCastArrayLenStub()
		for _, site := range f.sc.gcArrayLenStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
	if len(f.sc.gcFinalCastStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeDefinedCastStub()
		for _, site := range f.sc.gcFinalCastStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
	if len(f.sc.gcDefinedTestStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeDefinedTestStub()
		for _, site := range f.sc.gcDefinedTestStubSites {
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
		f.emitNativeBarrierSafeStructRefSetStub()
		for _, site := range f.sc.gcStructRefSetStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
	if len(f.sc.gcArrayRefSetStubSites) != 0 {
		stub := f.a.Len()
		f.emitNativeCardSafeArrayRefSetStub()
		for _, site := range f.sc.gcArrayRefSetStubSites {
			f.a.PatchRel32(site, stub)
		}
	}
}

// emitNativeArrayAllocStub consumes one generic native allocation reservation.
// Dynamic size arithmetic is widened by an explicit pre-shift bound, all
// reference initializers are validated before publication, and the complete
// payload is initialized before the handle's space byte becomes visible.
func (f *fn) emitNativeArrayAllocStub(site gcArrayAllocStubSite) {
	layout, ok := nativeGCArrayLayout(f.opt(optGCNativeAlloc), f.m, site.typeIndex)
	if !ok || site.mode == gcArrayNativeNone {
		panic("amd64: invalid native array allocation layout")
	}
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	a.Push(R8)
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	for _, reg := range [...]Reg{R12, R13, R14, R15} {
		a.Push(reg)
	}
	a.MovReg64(R12, R8) // synchronous control frame
	fallback := make([]int, 0, 32+int(site.count))
	addFallback := func(branch int) { fallback = append(fallback, branch) }

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)

	a.Load64(R10, R8, gc.NativeViewStructAllocStateOffset)
	a.TestSelf(R10, true)
	addFallback(a.JccPlaceholder(condE))
	a.Load64(RAX, R8, gc.NativeViewStructAllocEpochOffset)
	a.TestSelf(RAX, true)
	addFallback(a.JccPlaceholder(condE))
	a.Load32(RDX, RAX, 0)
	a.Load32(RAX, R10, gc.NativeStructAllocEpochOffset)
	a.Cmp32(RAX, RDX)
	addFallback(a.JccPlaceholder(condNE))
	a.Load32(RCX, R10, gc.NativeStructAllocCursorOffset)
	a.Load32(RAX, R10, gc.NativeStructAllocCountOffset)
	a.AluRI(cmpDigit, RAX, gc.NativeStructAllocHandleCapacity, false)
	addFallback(a.JccPlaceholder(condA))
	a.Cmp32(RCX, RAX)
	addFallback(a.JccPlaceholder(condAE))
	a.Load32(RSI, R10, gc.NativeStructAllocHandleBaseOffset)
	a.TestSelf(RSI, false)
	nonContiguousHandle := a.JccPlaceholder(condE)
	a.LeaScaledW(RSI, RSI, RCX, 0, 0, false)
	handleReady := a.JmpPlaceholder()
	a.PatchRel32(nonContiguousHandle, a.Len())
	a.LeaScaled(RAX, R10, RCX, 2, gc.NativeStructAllocHandlesOffset)
	a.Load32(RSI, RAX, 0)
	a.PatchRel32(handleReady, a.Len())
	a.TestSelf(RSI, false)
	addFallback(a.JccPlaceholder(condE))
	a.MovRegReg32(RAX, RSI)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	addFallback(a.JccPlaceholder(condAE))
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)
	a.Load32(RAX, R9, 16)
	a.ShiftImm(5, RAX, 16, false)
	a.AluRI(4, RAX, 0xff, false)
	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condNE))

	a.MovImm32(R13, int32(site.count))
	maxLength := uint32((uint64(math.MaxUint32) - uint64(gc.PayloadOffset) - 7) / uint64(layout.elemSize))
	a.AluRI(cmpDigit, R13, int32(maxLength), false)
	addFallback(a.JccPlaceholder(condA))
	a.MovRegReg32(R14, R13)
	switch layout.elemSize {
	case 2:
		a.ShiftImm(4, R14, 1, false)
	case 4:
		a.ShiftImm(4, R14, 2, false)
	case 8:
		a.ShiftImm(4, R14, 3, false)
	case 16:
		a.ShiftImm(4, R14, 4, false)
	}
	a.AluRI(0, R14, int32(gc.PayloadOffset+7), false)
	a.AluRI(4, R14, -8, false)
	a.Load32(RAX, R8, gc.NativeViewNurseryObjectMaxBytesOffset)
	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.Cmp32(R14, RAX)
	addFallback(a.JccPlaceholder(condAE))

	a.Load64(R10, R8, gc.NativeViewStructAllocStateOffset)
	a.Load32(R15, R10, gc.NativeStructAllocChunkCursorOffset)
	objectAlign := layout.elemAlign
	if objectAlign < 8 {
		objectAlign = 8
	}
	if objectAlign > 1 {
		a.AluRI(0, R15, int32(objectAlign-1), false)
		a.AluRI(4, R15, -int32(objectAlign), false)
	}
	a.Load32(RAX, R10, gc.NativeStructAllocChunkEndOffset)
	a.Cmp32(R15, RAX)
	addFallback(a.JccPlaceholder(condA))
	a.Sub32(RAX, R15)
	a.Cmp32(RAX, R14)
	addFallback(a.JccPlaceholder(condB))
	a.Load64(R11, R8, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBaseOffset))
	a.TestSelf(R11, true)
	addFallback(a.JccPlaceholder(condE))
	a.Add64(R11, R15)
	a.Load64(RAX, R8, gc.NativeViewAllocationCountOffset)
	a.TestSelf(RAX, true)
	addFallback(a.JccPlaceholder(condE))

	emitValidateRef := func(slot uint32) {
		a.Load32(RAX, R12, hcArgs+int32(slot*8))
		a.TestSelf(RAX, false)
		nullOK := -1
		if layout.nullable {
			nullOK = a.JccPlaceholder(condE)
		} else {
			addFallback(a.JccPlaceholder(condE))
		}
		a.MovRegReg32(RDX, RAX)
		a.AluRI(4, RDX, 1, false)
		a.TestSelf(RDX, false)
		i31OK := a.JccPlaceholder(condNE)
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
		a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceTiny), false)
		addFallback(a.JccPlaceholder(condA))
		valid := a.Len()
		if nullOK >= 0 {
			a.PatchRel32(nullOK, valid)
		}
		a.PatchRel32(i31OK, valid)
	}
	if layout.ref {
		switch site.mode {
		case gcArrayNativeUniform:
			emitValidateRef(0)
		case gcArrayNativeFixed:
			for i := uint32(0); i < site.count; i++ {
				emitValidateRef(i)
			}
		}
	}

	// Write header and payload before publishing the handle's nursery space.
	a.Load64(RAX, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(RAX, RAX, gc.NativeInstanceViewLocalTypesOffset)
	a.Load32(RAX, RAX, int32(site.typeIndex*4))
	a.Store32(R11, 0, RAX)
	a.Store32(R11, 4, R14)
	a.Store32(R11, 8, R13)
	flags := int32(0)
	if layout.pointerFree {
		flags = int32(gc.FlagPointerFree)
	}
	a.StoreImm32Mem(R11, 12, flags)

	switch site.mode {
	case gcArrayNativeDefault:
		a.MovReg64(RDI, R11)
		a.LeaDisp(RDI, RDI, int32(gc.PayloadOffset))
		a.MovRegReg32(RCX, R14)
		a.AluRI(5, RCX, int32(gc.PayloadOffset), false)
		a.TestSelf(RCX, false)
		zeroDone := a.JccPlaceholder(condE)
		a.AluRI(cmpDigit, RCX, 256, false)
		largeZero := a.JccPlaceholder(condAE)
		a.ShiftImm(5, RCX, 3, false)
		a.XorSelf32(RAX)
		zeroLoop := a.Len()
		a.Store64(RDI, 0, RAX)
		a.LeaDisp(RDI, RDI, 8)
		a.AluRI(5, RCX, 1, false)
		a.PatchRel32(a.JccPlaceholder(condNE), zeroLoop)
		zeroJoin := a.JmpPlaceholder()
		a.PatchRel32(largeZero, a.Len())
		a.XorSelf32(RAX)
		a.RepStosb()
		a.PatchRel32(zeroJoin, a.Len())
		a.PatchRel32(zeroDone, a.Len())
	case gcArrayNativeUniform:
		a.MovReg64(RDI, R11)
		a.LeaDisp(RDI, RDI, int32(gc.PayloadOffset))
		a.MovRegReg32(RCX, R13)
		a.XorSelf32(RDX)
		a.TestSelf(RCX, false)
		done := a.JccPlaceholder(condE)
		loop := a.Len()
		if layout.elemSize == 16 {
			a.Load64(RAX, R12, hcArgs)
			a.Store64(RDI, 0, RAX)
			a.Load64(RAX, R12, hcArgs+8)
			a.Store64(RDI, 8, RAX)
		} else if layout.elemSize == 8 {
			a.Load64(RAX, R12, hcArgs)
			a.Store64(RDI, 0, RAX)
		} else {
			a.Load32(RAX, R12, hcArgs)
			a.StoreIdx(RDI, RDX, RAX, 0, int(layout.elemSize))
		}
		a.LeaDisp(RDI, RDI, int32(layout.elemSize))
		a.AluRI(5, RCX, 1, false)
		a.PatchRel32(a.JccPlaceholder(condNE), loop)
		a.PatchRel32(done, a.Len())
	case gcArrayNativeFixed:
		a.XorSelf32(RDX)
		for i := uint32(0); i < site.count; i++ {
			src := hcArgs + int32(i*layout.valueSlots*8)
			dst := int32(gc.PayloadOffset + i*layout.elemSize)
			switch layout.elemSize {
			case 1, 2:
				a.Load32(RAX, R12, src)
				a.StoreIdx(R11, RDX, RAX, dst, int(layout.elemSize))
			case 4:
				a.Load32(RAX, R12, src)
				a.Store32(R11, dst, RAX)
			case 8:
				a.Load64(RAX, R12, src)
				a.Store64(R11, dst, RAX)
			case 16:
				a.Load64(RAX, R12, src)
				a.Store64(R11, dst, RAX)
				a.Load64(RAX, R12, src+8)
				a.Store64(R11, dst+8, RAX)
			}
		}
	}

	a.Store32(R9, gc.NativeHandleOffsetOffset, R15)
	a.Store32(R9, gc.NativeHandleSizeOffset, R14)
	a.Store32(R9, 8, R14)
	a.StoreImm32Mem(R9, gc.NativeHandleCardSlotOffset, 0)
	a.MovRegReg32(RAX, R15)
	a.AluRR(0x01, RAX, R14, false)
	a.Load64(R10, R8, gc.NativeViewStructAllocStateOffset)
	a.Store32(R10, gc.NativeStructAllocChunkCursorOffset, RAX)
	a.StoreImm32Mem(R9, 16, int32(gc.NativeSpaceNursery)<<16)
	a.Load32(RDX, R10, gc.NativeStructAllocCursorOffset)
	a.AluRI(0, RDX, 1, false)
	a.Store32(R10, gc.NativeStructAllocCursorOffset, RDX)
	a.Load64(R10, R8, gc.NativeViewAllocationCountOffset)
	a.Load64(RAX, R10, 0)
	a.AluRI(0, RAX, 1, true)
	a.Store64(R10, 0, RAX)
	a.MovRegReg32(RAX, RSI)
	a.ShiftImm(4, RAX, 1, false)
	a.Store64(R12, hcResults, RAX)
	done := a.JmpPlaceholder()

	fallbackAt := a.Len()
	for _, branch := range fallback {
		a.PatchRel32(branch, fallbackAt)
	}
	a.MovImm32(RAX, 0)
	a.PatchRel32(done, a.Len())
	a.TestSelf(RAX, false)
	for i := 3; i >= 0; i-- {
		a.Pop(R12 + Reg(i))
	}
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Pop(R8)
	a.Ret()
}

// emitNativeStructAllocStub consumes one collector-reserved handle identity and
// advances the private nursery-chunk cursor only after every constructor reference
// has been validated. It writes the result directly to the synchronous control frame and
// returns nonzero in RAX on success; zero selects the ordinary rooted helper.
func (f *fn) emitNativeStructAllocStub(typeIndex uint32) {
	plan, ok := f.nativeGCStructAllocLayout(typeIndex)
	if !ok {
		panic("amd64: invalid native struct allocation layout")
	}
	fields, objectSize, objectAlign, pointerFree := plan.fields, plan.objectSize, plan.objectAlign, plan.pointerFree
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	a.Push(R8)
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	a.MovReg64(RDI, R8) // retain the synchronous control frame
	fallback := make([]int, 0, 20+len(fields)*2)
	addFallback := func(site int) { fallback = append(fallback, site) }

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.Load32(RDX, R10, int32(typeIndex*4))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)

	a.Load64(R10, R8, gc.NativeViewStructAllocStateOffset)
	a.TestSelf(R10, true)
	addFallback(a.JccPlaceholder(condE))
	a.Load64(RAX, R8, gc.NativeViewStructAllocEpochOffset)
	a.TestSelf(RAX, true)
	addFallback(a.JccPlaceholder(condE))
	a.Load32(RDX, RAX, 0)
	a.Load32(RAX, R10, gc.NativeStructAllocEpochOffset)
	a.Cmp32(RAX, RDX)
	addFallback(a.JccPlaceholder(condNE))
	a.Load32(RCX, R10, gc.NativeStructAllocCursorOffset)
	a.Load32(RAX, R10, gc.NativeStructAllocCountOffset)
	a.AluRI(cmpDigit, RAX, gc.NativeStructAllocHandleCapacity, false)
	addFallback(a.JccPlaceholder(condA))
	a.Cmp32(RCX, RAX)
	addFallback(a.JccPlaceholder(condAE))
	a.LeaScaled(RAX, R10, RCX, 2, gc.NativeStructAllocHandlesOffset)
	a.Load32(RSI, RAX, 0)
	a.TestSelf(RSI, false)
	addFallback(a.JccPlaceholder(condE))
	a.MovRegReg32(RAX, RSI)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewHandleCountOffset, false)
	addFallback(a.JccPlaceholder(condAE))
	a.Load64(R9, R8, gc.NativeViewHandlesOffset)
	a.ImulRI(RAX, gc.NativeHandleStride, true)
	a.Add64(R9, RAX)
	a.Load32(RAX, R9, 16)
	a.ShiftImm(5, RAX, 16, false)
	a.AluRI(4, RAX, 0xff, false)
	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condNE))

	a.Load64(RAX, R8, gc.NativeViewNurseryBumpOffset)
	a.TestSelf(RAX, true)
	addFallback(a.JccPlaceholder(condE))
	a.Load32(RCX, RAX, 0)
	if objectAlign > 1 {
		a.AluRI(0, RCX, int32(objectAlign-1), false)
		a.AluRI(4, RCX, -int32(objectAlign), false)
	}
	a.Load32(RAX, R8, gc.NativeViewNurseryAllocBytesOffset)
	a.Cmp32(RCX, RAX)
	addFallback(a.JccPlaceholder(condA))
	a.Sub32(RAX, RCX)
	a.AluRI(cmpDigit, RAX, int32(objectSize), false)
	addFallback(a.JccPlaceholder(condB))
	a.Load64(R11, R8, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceNursery*gc.NativeViewSpaceStride+gc.NativeSpaceBaseOffset))
	a.TestSelf(R11, true)
	addFallback(a.JccPlaceholder(condE))
	a.Add64(R11, RCX)
	a.Load64(RAX, R8, gc.NativeViewAllocationCountOffset)
	a.TestSelf(RAX, true)
	addFallback(a.JccPlaceholder(condE))

	for _, field := range fields {
		if !field.CollectorRef {
			continue
		}
		a.Load32(RAX, RDI, hcArgs+int32(field.Slot*8))
		a.TestSelf(RAX, false)
		nullOK := -1
		if field.Nullable {
			nullOK = a.JccPlaceholder(condE)
		} else {
			addFallback(a.JccPlaceholder(condE))
		}
		a.MovRegReg32(RDX, RAX)
		a.AluRI(4, RDX, 1, false)
		a.TestSelf(RDX, false)
		i31OK := a.JccPlaceholder(condNE)
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
		a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceTiny), false)
		addFallback(a.JccPlaceholder(condA))
		valid := a.Len()
		if nullOK >= 0 {
			a.PatchRel32(nullOK, valid)
		}
		a.PatchRel32(i31OK, valid)
	}

	// Complete object bytes before publishing the handle's nursery-space byte.
	a.Load64(R10, R8, gc.NativeViewStructAllocStateOffset)
	a.Load32(RDX, R10, gc.NativeStructAllocCursorOffset)
	a.Load64(RAX, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(RAX, RAX, gc.NativeInstanceViewLocalTypesOffset)
	a.Load32(RAX, RAX, int32(typeIndex*4))
	a.Store32(R11, 0, RAX)
	a.StoreImm32Mem(R11, 4, int32(objectSize))
	a.StoreImm32Mem(R11, 8, 0)
	flags := int32(0)
	if pointerFree {
		flags = int32(gc.FlagPointerFree)
	}
	a.StoreImm32Mem(R11, 12, flags)
	for _, field := range fields {
		disp := int32(gc.PayloadOffset + field.Offset)
		src := hcArgs + int32(field.Slot*8)
		switch field.Size {
		case 4:
			a.Load32(RAX, RDI, src)
			a.Store32(R11, disp, RAX)
		case 8:
			a.Load64(RAX, RDI, src)
			a.Store64(R11, disp, RAX)
		case 16:
			a.Load64(RAX, RDI, src)
			a.Store64(R11, disp, RAX)
			a.Load64(RAX, RDI, src+8)
			a.Store64(R11, disp+8, RAX)
		default:
			panic("amd64: unsupported native struct field size")
		}
	}
	a.Store32(R9, gc.NativeHandleOffsetOffset, RCX)
	a.StoreImm32Mem(R9, gc.NativeHandleSizeOffset, int32(objectSize))
	a.StoreImm32Mem(R9, 8, int32(objectSize))
	a.StoreImm32Mem(R9, gc.NativeHandleCardSlotOffset, 0)
	a.Load64(RAX, R8, gc.NativeViewNurseryBumpOffset)
	a.MovRegReg32(R10, RCX)
	a.AluRI(0, R10, int32(objectSize), false)
	a.Store32(RAX, 0, R10)
	a.StoreImm32Mem(R9, 16, int32(gc.NativeSpaceNursery)<<16)
	a.AluRI(0, RDX, 1, false)
	a.Load64(R10, R8, gc.NativeViewStructAllocStateOffset)
	a.Store32(R10, gc.NativeStructAllocCursorOffset, RDX)
	a.Load64(R10, R8, gc.NativeViewAllocationCountOffset)
	a.Load64(RAX, R10, 0)
	a.AluRI(0, RAX, 1, true)
	a.Store64(R10, 0, RAX)
	a.MovRegReg32(RAX, RSI)
	a.ShiftImm(4, RAX, 1, false)
	a.Store64(RDI, hcResults, RAX)
	done := a.JmpPlaceholder()

	fallbackAt := a.Len()
	for _, site := range fallback {
		a.PatchRel32(site, fallbackAt)
	}
	a.MovImm32(RAX, 0)
	a.PatchRel32(done, a.Len())
	a.TestSelf(RAX, false) // RET and POP preserve flags for the caller's JNE
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Pop(R8)
	a.Ret()
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
	for _, state := range f.globalReg {
		reg := globalRegValue(state)
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

	// Instantiation proved the immutable view ABI and local type-map shape.
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	// Resolve and semantically validate the compact handle through current backing.
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
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

// emitNativeDefinedCastStub validates one defined collector cast through exact
// canonical identity or the collector's immutable subtype interval table.
func (f *fn) emitNativeDefinedCastStub() { f.emitNativeDefinedTypeCheckStub(false) }

// emitNativeDefinedTestStub returns the dynamic defined-type test result without
// entering Go. Invalid compact references remain fail-closed traps.
func (f *fn) emitNativeDefinedTestStub() { f.emitNativeDefinedTypeCheckStub(true) }

// emitNativeDefinedTypeCheckStub consumes EAX=compact reference,
// EDX=module-local target type, ECX=nullable flag, and ESI=exact flag. Test mode
// returns zero/one in EAX; cast mode returns the original compact reference.
func (f *fn) emitNativeDefinedTypeCheckStub(test bool) {
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}

	mismatches := make([]int, 0, 3)
	failIf := func(cond Cond) {
		if test {
			mismatches = append(mismatches, a.JccPlaceholder(cond))
		} else {
			f.trapIf(cond, trapCastFailure)
		}
	}

	a.TestSelf(RAX, false)
	nonNull := a.JccPlaceholder(condNE)
	if test {
		a.MovRegReg32(RAX, RCX)
	} else {
		a.TestSelf(RCX, false)
		f.trapIf(condE, trapCastFailure)
	}
	nullDone := a.JmpPlaceholder()
	a.PatchRel32(nonNull, a.Len())

	if !test {
		a.MovRegReg32(RDI, RAX) // retain the original compact reference
	}
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	failIf(condNE) // i31 cannot satisfy a defined struct/array target

	// Resolve the module-local target into the collector's canonical domain.
	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	// Resolve and validate the compact handle against the current heap backing.
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
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
	a.Load32(RCX, R11, 0) // actual canonical type ID

	// Exact casts compare canonical IDs. Ordinary casts/tests use DFS interval
	// containment: required.pre <= actual.pre && actual.post <= required.post.
	a.TestSelf(RSI, false)
	nonExact := a.JccPlaceholder(condE)
	a.Cmp32(RCX, RDX)
	failIf(condNE)
	exactDone := a.JmpPlaceholder()
	a.PatchRel32(nonExact, a.Len())

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
	a.MovRegReg32(RAX, RCX)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewSubtypeIntervalCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	a.MovRegReg32(RAX, RDX)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewSubtypeIntervalCountOffset, false)
	f.trapIf(condAE, trapCastFailure)
	a.Load64(R10, R8, gc.NativeViewSubtypeIntervalsOffset)
	a.TestSelf(R10, true)
	f.trapIf(condE, trapCastFailure)
	a.MovRegReg32(RAX, RCX)
	a.ImulRI(RAX, 8, true)
	a.LoadIdx(R9, R10, RAX, 0, 8, false, true)
	a.MovRegReg32(RAX, RDX)
	a.ImulRI(RAX, 8, true)
	a.LoadIdx(R10, R10, RAX, 0, 8, false, true)
	a.MovReg64(RAX, R9)
	a.ShiftImm(5, RAX, 32, true)
	a.MovReg64(RCX, R10)
	a.ShiftImm(5, RCX, 32, true)
	a.Cmp32(RAX, RCX)
	failIf(condB)
	a.Cmp32(R9, R10)
	failIf(condA)

	successAt := a.Len()
	a.PatchRel32(exactDone, successAt)
	if test {
		a.MovImm32(RAX, 1)
	} else {
		a.MovRegReg32(RAX, RDI)
	}
	done := a.JmpPlaceholder()
	if test {
		mismatchAt := a.Len()
		a.MovImm32(RAX, 0)
		for _, branch := range mismatches {
			a.PatchRel32(branch, mismatchAt)
		}
	}
	finish := a.Len()
	a.PatchRel32(nullDone, finish)
	a.PatchRel32(done, finish)
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
	for _, state := range f.globalReg {
		reg := globalRegValue(state)
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
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
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
	for _, state := range f.globalReg {
		reg := globalRegValue(state)
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
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
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

// emitNativeBarrierSafeStructRefSetStub resolves an exact final struct in
// Throughput nursery, old, or large space. Nursery stores need no barrier;
// old/large stores proceed only for non-young children or an already remembered
// parent. Tiny and stores that must grow the remembered set use the exact Go
// helper fallback. The checked store returns EAX=1; EAX=0 selects that fallback.
func (f *fn) emitNativeBarrierSafeStructRefSetStub() {
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, state := range f.globalReg {
		reg := globalRegValue(state)
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
	var fallback [16]int
	nfallback := 0
	addFallback := func(site int) { fallback[nfallback], nfallback = site, nfallback+1 }

	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	addFallback(a.JccPlaceholder(condNE))

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
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
	addFallback(a.JccPlaceholder(condB))
	a.AluRI(cmpDigit, R10, int32(gc.NativeSpaceLarge), false)
	addFallback(a.JccPlaceholder(condA))

	// Nursery, old, and large objects all have directly addressable metadata.
	// Tiny remains helper-bound because its incremental barrier is stateful.
	a.MovRegReg32(RAX, R10)
	a.ImulRI(RAX, gc.NativeViewSpaceStride, true)
	a.LoadIdx(R11, R8, RAX, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceBaseOffset), 8, false, true)
	a.LoadIdx(R10, R8, RAX, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceBytesOffset), 4, false, false)
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
	// Old/large parents may bypass the Go barrier when the child is not young,
	// or when the parent is already conservatively remembered.
	a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceNursery), false)
	childIsNursery := a.JccPlaceholder(condE)
	a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceLarge), false)
	addFallback(a.JccPlaceholder(condE)) // large children may still be young-in-place
	childNotYoung := a.JmpPlaceholder()
	a.PatchRel32(childIsNursery, a.Len())
	a.Load32(RAX, R9, 16)
	a.ShiftImm(5, RAX, 16, false)
	a.AluRI(4, RAX, 0xff, false)
	a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceNursery), false)
	parentIsYoung := a.JccPlaceholder(condE)
	a.Load32(RAX, R9, 16)
	a.ShiftImm(5, RAX, 24, false)
	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.PatchRel32(childValid, a.Len())
	a.PatchRel32(childI31, a.Len())
	a.PatchRel32(childNotYoung, a.Len())
	a.PatchRel32(parentIsYoung, a.Len())
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

// emitNativeCardSafeArrayRefSetStub extends nursery stores to Throughput
// old/large arrays only when remembered membership and an existing object card
// are already present. It may widen that stable card interval in place, but it
// never grows collector metadata; cardless, unremembered, and Tiny stores retain
// the exact helper fallback.
func (f *fn) emitNativeCardSafeArrayRefSetStub() {
	a := f.a
	var preserve [3]bool
	for _, local := range f.pinnedLocals {
		if reg := f.locals[local].reg; reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for _, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg >= R9 && reg <= R11 {
			preserve[reg-R9] = true
		}
	}
	for i, yes := range preserve {
		if yes {
			a.Push(R9 + Reg(i))
		}
	}
	a.Push(RAX)             // compact parent for object-card identity validation
	a.MovRegReg32(RDI, RSI) // compact child
	a.MovRegReg32(RSI, RCX) // array index
	var fallback [24]int
	nfallback := 0
	addFallback := func(site int) { fallback[nfallback], nfallback = site, nfallback+1 }

	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.MovRegReg32(R10, RAX)
	a.AluRI(4, R10, 1, false)
	a.TestSelf(R10, false)
	addFallback(a.JccPlaceholder(condNE))

	a.Load64(R8, RBX, -int32(abi.GCNativeViewPtrOffset))
	a.Load64(R10, R8, gc.NativeInstanceViewLocalTypesOffset)
	a.ImulRI(RDX, 4, true)
	a.LoadIdx(RDX, R10, RDX, 0, 4, false, false)

	a.Load64(R8, R8, gc.NativeInstanceViewCollectorOffset)
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
	addFallback(a.JccPlaceholder(condB))
	a.AluRI(cmpDigit, R10, int32(gc.NativeSpaceLarge), false)
	addFallback(a.JccPlaceholder(condA))

	a.MovRegReg32(RAX, R10)
	a.ImulRI(RAX, gc.NativeViewSpaceStride, true)
	a.LoadIdx(R11, R8, RAX, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceBaseOffset), 8, false, true)
	a.LoadIdx(R10, R8, RAX, int32(gc.NativeViewSpacesOffset+gc.NativeSpaceBytesOffset), 4, false, false)
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
	a.MovRegReg32(RCX, RSI)
	a.ShiftImm(5, RCX, 2, false) // restore the logical array index for card metadata

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
	a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceNursery), false)
	childIsNursery := a.JccPlaceholder(condE)
	a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceLarge), false)
	addFallback(a.JccPlaceholder(condE)) // large children may still be young-in-place
	childNotYoung := a.JmpPlaceholder()
	a.PatchRel32(childIsNursery, a.Len())
	// A nursery child behind an old/large parent requires established remembered
	// membership before the native path may update an existing card.
	a.Load32(RAX, R9, 16)
	a.ShiftImm(5, RAX, 16, false)
	a.AluRI(4, RAX, 0xff, false)
	a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceNursery), false)
	parentNurseryYoungChild := a.JccPlaceholder(condE)
	a.Load32(RAX, R9, 16)
	a.ShiftImm(5, RAX, 24, false)
	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))

	cardCheck := a.Len()
	a.PatchRel32(childValid, cardCheck)
	a.PatchRel32(childI31, cardCheck)
	a.PatchRel32(childNotYoung, cardCheck)
	a.Load32(RAX, R9, 16)
	a.ShiftImm(5, RAX, 16, false)
	a.AluRI(4, RAX, 0xff, false)
	a.AluRI(cmpDigit, RAX, int32(gc.NativeSpaceNursery), false)
	parentNursery := a.JccPlaceholder(condE)

	// Old/large arrays may proceed only when the helper has already allocated a
	// valid fixed-card range covering this payload byte. A store in another card
	// falls back so Go can append or coalesce linked metadata without relocating it
	// under native code.
	a.Load32(RAX, R9, gc.NativeHandleCardSlotOffset)
	a.TestSelf(RAX, false)
	addFallback(a.JccPlaceholder(condE))
	a.AluRI(5, RAX, 1, false)
	a.AluRM(cmpRMcode, RAX, R8, gc.NativeViewObjectCardCountOffset, false)
	addFallback(a.JccPlaceholder(condAE))
	a.Load64(R10, R8, gc.NativeViewObjectCardsOffset)
	a.TestSelf(R10, true)
	addFallback(a.JccPlaceholder(condE))
	a.ImulRI(RAX, gc.NativeObjectCardStride, true)
	a.Add64(R10, RAX)
	a.Load32(RAX, R10, gc.NativeObjectCardHandleOffset)
	a.Load32(RDX, RSP, 0)
	a.ShiftImm(5, RDX, 1, false)
	a.Cmp32(RAX, RDX)
	addFallback(a.JccPlaceholder(condNE))
	a.Load32(RAX, R10, gc.NativeObjectCardStartOffset)
	a.Cmp32(RSI, RAX)
	addFallback(a.JccPlaceholder(condB))
	a.Load32(RAX, R10, gc.NativeObjectCardEndOffset)
	a.Cmp32(RSI, RAX)
	addFallback(a.JccPlaceholder(condA))

	store := a.Len()
	a.PatchRel32(parentNurseryYoungChild, store)
	a.PatchRel32(parentNursery, store)
	a.StoreIdx(R11, RSI, RDI, int32(gc.PayloadOffset), 4)
	a.MovImm32(RAX, 1)
	done := a.JmpPlaceholder()

	fallbackAt := a.Len()
	for i := 0; i < nfallback; i++ {
		a.PatchRel32(fallback[i], fallbackAt)
	}
	a.MovImm32(RAX, 0)
	a.PatchRel32(done, a.Len())
	a.Pop(RDX)
	for i := len(preserve) - 1; i >= 0; i-- {
		if preserve[i] {
			a.Pop(R9 + Reg(i))
		}
	}
	a.Ret()
}

func (f *fn) emitDirectGCStructGet(typeIndex, fieldIndex uint32, helper uint32) bool {
	off, scalar, final, ok := f.directGCStructLayout(typeIndex, fieldIndex)
	if !ok || !final {
		return false
	}
	local, hasLocal := gcLocalProvenance(f.s.back())
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	object := f.popValue()
	required := gc.PayloadOffset + off + uint32(scalar.size)
	obj, done := f.emitDirectGCObject(object, typeIndex, required, local, hasLocal)
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
	off, scalar, final, ok := f.directGCStructLayout(typeIndex, fieldIndex)
	if !ok || !final {
		return false
	}
	valueRoot := f.s.back()
	local, hasLocal := gcLocalProvenance(baseOfValentBlock(valueRoot).prev)
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	value := f.popValue()
	object := f.popValue()
	required := gc.PayloadOffset + off + uint32(scalar.size)
	obj, done := f.emitDirectGCObject(object, typeIndex, required, local, hasLocal)
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

func (f *fn) emitDirectGCArrayLen(typeIndex uint32) bool {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok {
		if !layout.DirectLen {
			return false
		}
	} else if target, ok := f.stagedArraySubtype(typeIndex); !ok || !target.Final {
		// Legacy low-level tests may omit compiler-only #365 layouts. Production
		// compilation always takes the precomputed metadata branch above.
		return false
	}
	local, hasLocal := gcLocalProvenance(f.s.back())
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	object := f.popValue()
	obj, done := f.emitDirectGCObject(object, typeIndex, gc.HeaderSize, local, hasLocal)
	result := f.allocReg(maskOf(obj))
	f.a.Load32(result, obj, 8)
	done()
	f.spillFloor = oldFloor
	f.pushReg(result, mtI32)
	return true
}

func (f *fn) emitDirectGCArrayGet(typeIndex uint32, helper uint32, knownIndex, knownLength uint32, logicalBoundsProven bool) bool {
	scalar, final, ok := f.directGCArrayLayout(typeIndex)
	if !ok || !final {
		return false
	}
	indexRoot := f.s.back()
	local, hasLocal := gcLocalProvenance(baseOfValentBlock(indexRoot).prev)
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	indexValue := f.popValue()
	object := f.popValue()
	constantOffset := uint64(gc.PayloadOffset) + uint64(knownIndex)*uint64(scalar.size)
	constantExtent := constantOffset + uint64(scalar.size)
	if logicalBoundsProven && constantExtent <= math.MaxInt32 {
		obj, done := f.emitDirectGCObject(object, typeIndex, uint32(constantExtent), local, hasLocal)
		tmp := f.allocReg(maskOf(obj))
		f.a.Load32(tmp, obj, 8)
		f.a.AluRI(cmpDigit, tmp, int32(knownLength), false)
		f.trapIf(condNE, trapCastFailure)
		f.release(tmp)
		disp := int32(constantOffset)
		f.stats.peep("gc-array-known-bounds")
		f.stats.peep("gc-array-const-index")
		if scalar.typ.isFloat() {
			x := f.allocFReg(0)
			f.a.FLoadDisp(x, obj, disp, scalar.typ == mtF64)
			done()
			f.spillFloor = oldFloor
			f.pushFReg(x, scalar.typ)
			return true
		}
		result := f.allocReg(maskOf(obj))
		f.a.LoadIdx(result, obj, RSP, disp, scalar.size, helper == gcArrayGetS, scalar.typ == mtI64)
		done()
		f.spillFloor = oldFloor
		f.pushReg(result, scalar.typ)
		return true
	}

	obj, done := f.emitDirectGCObject(object, typeIndex, gc.PayloadOffset, local, hasLocal)
	index := f.materialize(indexValue)
	f.a.MovRegReg32(index, index) // Wasm i32 indexes ignore dirty host-result high bits.
	f.pinned = f.pinned.add(index)
	tmp := f.allocReg(maskOf(obj, index))
	f.pinned = f.pinned.add(tmp)
	f.a.Load32(tmp, obj, 8) // ObjHeader.Aux array length
	if logicalBoundsProven {
		f.a.AluRI(cmpDigit, tmp, int32(knownLength), false)
		f.trapIf(condNE, trapCastFailure)
		f.stats.peep("gc-array-known-bounds")
	} else {
		f.a.Cmp32(index, tmp)
		f.trapIf(condAE, trapBuiltin)
	}
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

func (f *fn) emitDirectGCArraySet(typeIndex, knownIndex, knownLength uint32, logicalBoundsProven bool) bool {
	scalar, final, ok := f.directGCArrayLayout(typeIndex)
	if !ok || !final {
		return false
	}
	valueRoot := f.s.back()
	indexRoot := baseOfValentBlock(valueRoot).prev
	local, hasLocal := gcLocalProvenance(baseOfValentBlock(indexRoot).prev)
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	value := f.popValue()
	indexValue := f.popValue()
	object := f.popValue()
	constantOffset := uint64(gc.PayloadOffset) + uint64(knownIndex)*uint64(scalar.size)
	constantExtent := constantOffset + uint64(scalar.size)
	if logicalBoundsProven && constantExtent <= math.MaxInt32 {
		obj, done := f.emitDirectGCObject(object, typeIndex, uint32(constantExtent), local, hasLocal)
		tmp := f.allocReg(maskOf(obj))
		f.a.Load32(tmp, obj, 8)
		f.a.AluRI(cmpDigit, tmp, int32(knownLength), false)
		f.trapIf(condNE, trapCastFailure)
		f.release(tmp)
		disp := int32(constantOffset)
		f.stats.peep("gc-array-known-bounds")
		f.stats.peep("gc-array-const-index")
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

	obj, done := f.emitDirectGCObject(object, typeIndex, gc.PayloadOffset, local, hasLocal)
	index := f.materialize(indexValue)
	f.a.MovRegReg32(index, index) // Wasm i32 indexes ignore dirty host-result high bits.
	f.pinned = f.pinned.add(index)
	tmp := f.allocReg(maskOf(obj, index))
	f.pinned = f.pinned.add(tmp)
	f.a.Load32(tmp, obj, 8)
	if logicalBoundsProven {
		f.a.AluRI(cmpDigit, tmp, int32(knownLength), false)
		f.trapIf(condNE, trapCastFailure)
		f.stats.peep("gc-array-known-bounds")
	} else {
		f.a.Cmp32(index, tmp)
		f.trapIf(condAE, trapBuiltin)
	}
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
