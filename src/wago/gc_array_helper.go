package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

const (
	gcArrayAllocDefault        uint32 = 16
	gcArrayGet                 uint32 = 17
	gcArrayGetS                uint32 = 18
	gcArrayGetU                uint32 = 19
	gcArraySet                 uint32 = 20
	gcArrayLen                 uint32 = 21
	gcArrayAllocFixed          uint32 = 22
	gcArrayAllocUniform        uint32 = 23
	gcArrayAllocData           uint32 = 24
	gcArrayAllocElem           uint32 = 25
	gcArrayDropElem            uint32 = 26
	gcArrayFill                uint32 = 27
	gcArrayCopy                uint32 = 28
	gcArrayInitData            uint32 = 29
	gcArrayInitElem            uint32 = 30
	gcArrayAllocFixedV128Spill uint32 = 31
	gcArrayAllocDefaultNative  uint32 = 32
	gcArrayAllocUniformNative  uint32 = 33
	gcArrayAllocFixedNative    uint32 = 34
	gcArrayCheckDefault        uint32 = 36
	gcArrayCheckUniform        uint32 = 37
	gcArrayCheckData           uint32 = 38
	gcArrayCheckFixed          uint32 = 39
)

func gcArrayElementStorage(kind gc.StorageKind) bool {
	switch kind {
	case gc.StorageRef, gc.StorageRefNull,
		gc.StorageFuncRef, gc.StorageFuncRefNull,
		gc.StorageExternRef, gc.StorageExternRefNull:
		return true
	default:
		return false
	}
}

//lint:ignore U1000 retained as the unparked native helper entrypoint
func (in *Instance) dispatchGCHelper(helper uint32, args, results []uint64) {
	in.dispatchGCHelperParked(0, helper, 0, args, results)
}

func (in *Instance) dispatchGCHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64) {
	if helper < gcArrayAllocDefault {
		in.dispatchGCStructHelperParked(ctrl, helper, safepoint, args, results)
		return
	}
	in.dispatchGCArrayHelperParked(ctrl, helper, safepoint, args, results)
}

func (in *Instance) dispatchGCArrayHelper(helper uint32, args, results []uint64) {
	in.dispatchGCArrayHelperParked(0, helper, 0, args, results)
}

func (in *Instance) dispatchGCArrayHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64) {
	if in == nil || in.gc == nil {
		panic(gcStructHelperError{err: fmt.Errorf("gc array helper %d has no live collector", helper)})
	}
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	recordSynchronousGCHelper(in, helper, args)
	var state *gcPublicState
	var frameRoots gc.RootSet = gc.EmptyRoots{}
	if gcHelperMayAllocate(helper) {
		in.gc.CancelNativeAllocationBatch()
		state = in.publicGCState()
		state.mu.Lock()
		defer state.mu.Unlock()
		frameRoots = in.gcHelperRoots(ctrl, state, safepoint)
	}

	checkArray := func(ref gc.Ref, typeID uint32) {
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		actual, err := in.gc.ObjectType(ref)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if !in.gcObjectTypeMatches(actual, typeID) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type = %d, want subtype of %d", actual, typeID)})
		}
	}
	arrayElemKind := func(typeID uint32) gc.StorageKind {
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type %d is unavailable", typeID)})
		}
		return in.c.GCTypeDescs[typeID].Elem
	}
	arrayValueSlots := func(typeID uint32) int {
		if arrayElemKind(typeID) == gc.StorageV128 {
			return 2
		}
		return 1
	}
	arrayValue := func(typeID uint32, words []uint64) gc.Value {
		kind := arrayElemKind(typeID)
		if kind == gc.StorageRef || kind == gc.StorageRefNull {
			panic(gcStructHelperError{err: fmt.Errorf("gc array reference elements remain outside the staged helper slice")})
		}
		wantSlots := 1
		if kind == gc.StorageV128 {
			wantSlots = 2
		}
		if len(words) != wantSlots {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type %d value uses %d slot(s), want %d", typeID, len(words), wantSlots)})
		}
		valueKind := kind
		if kind == gc.StorageI8 || kind == gc.StorageI16 {
			valueKind = gc.StorageI32
		}
		value := gc.Value{Kind: valueKind, Bits: words[0]}
		if kind == gc.StorageV128 {
			value.BitsHi = words[1]
		}
		return value
	}
	arrayRefValue := func(typeID uint32, bits uint64) gc.Value {
		if int(typeID) >= len(in.c.Types) || in.c.Types[typeID].Kind != CompositeTypeArray || in.c.Types[typeID].Array.Storage.Value.Kind != ValueTypeReference {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type %d has no reference element descriptor", typeID)})
		}
		ref := gc.Ref(uint32(bits))
		want := in.c.Types[typeID].Array.Storage.Value
		if ref.IsNull() {
			if !want.Ref.Nullable {
				panic(gcStructHelperError{err: fmt.Errorf("gc array type %d rejects null reference element", typeID)})
			}
			return gc.RefValue(ref)
		}
		if !in.gcRefMatchesValueType(ref, want) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array reference does not match destination type %d", typeID)})
		}
		return gc.RefValue(ref)
	}
	arrayStoredValue := func(typeID uint32, words []uint64) gc.Value {
		kind := arrayElemKind(typeID)
		if kind == gc.StorageRef || kind == gc.StorageRefNull {
			if len(words) != 1 {
				panic(gcStructHelperError{err: fmt.Errorf("gc array reference value uses %d slots", len(words))})
			}
			return arrayRefValue(typeID, words[0])
		}
		return arrayValue(typeID, words)
	}
	arrayElemSegment := func(elemIndex uint32) (entries uintptr, length uint32, entryBytes int) {
		if int(elemIndex) >= len(in.c.passiveElems) || in.jm.PassiveElemPtr() == 0 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array element segment %d is unavailable", elemIndex)})
		}
		descOff := int(elemIndex) * coreruntime.PassiveElemDescBytes
		desc := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr()+uintptr(descOff))), coreruntime.PassiveElemDescBytes)
		length = binary.LittleEndian.Uint32(desc[8:])
		if uint64(length) > uint64(len(in.c.passiveElems[elemIndex].Values)) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array element segment %d length %d exceeds retained entries %d", elemIndex, length, len(in.c.passiveElems[elemIndex].Values))})
		}
		entries = uintptr(binary.LittleEndian.Uint64(desc))
		if length != 0 && entries == 0 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array element segment %d has no entries", elemIndex)})
		}
		entryBytes = elemEntryBytes(in.c.passiveElems[elemIndex].RefType)
		return
	}
	arrayElemBits := func(entries uintptr, entryBytes int, index uint32) uint64 {
		off := uint64(index) * uint64(entryBytes)
		if off > uint64(^uintptr(0))-uint64(entries) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array element entry %d address overflows", index)})
		}
		entry := unsafe.Slice((*byte)(offHeapPtr(entries+uintptr(off))), entryBytes)
		if entryBytes == coreruntime.TableEntryBytes {
			return binary.LittleEndian.Uint64(entry[coreruntime.TableEntryRefSlotOffset:])
		}
		return binary.LittleEndian.Uint64(entry)
	}
	arrayElemValue := func(typeID uint32, entries uintptr, entryBytes int, index uint32) gc.Value {
		return arrayStoredValue(typeID, []uint64{arrayElemBits(entries, entryBytes, index)})
	}
	arrayElemValuePrevalidated := func(typeID uint32, entries uintptr, entryBytes int, index uint32) gc.Value {
		bits := arrayElemBits(entries, entryBytes, index)
		kind := arrayElemKind(typeID)
		if kind == gc.StorageRef || kind == gc.StorageRefNull {
			return gc.RefValue(gc.Ref(uint32(bits)))
		}
		return gc.Value{Kind: kind, Bits: bits}
	}
	panicArrayError := func(err error) {
		if errors.Is(err, gc.ErrAllocationTooLarge) {
			panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
		}
		panic(gcStructHelperError{err: err})
	}

	switch helper {
	case gcArrayCheckFixed:
		if len(args) != 2 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array check-fixed helper arity = %d, want 2", len(args))})
		}
		ref, err := in.gc.ReserveDeadArrayAllocation(in.requireGCDomainType(uint32(args[0])), uint32(args[1]), frameRoots)
		if err != nil {
			panicArrayError(err)
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcArrayCheckDefault:
		if len(args) != 2 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array check-default helper arity = %d, want 2", len(args))})
		}
		ref, err := in.gc.ReserveDeadDefaultArrayAllocation(in.requireGCDomainType(uint32(args[1])), uint32(args[0]), frameRoots)
		if err != nil {
			panicArrayError(err)
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcArrayCheckUniform:
		if len(args) < 3 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array check-uniform helper arity = %d, want at least 3", len(args))})
		}
		typeID := uint32(args[len(args)-1])
		if gcArrayElementStorage(arrayElemKind(typeID)) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array dead-uniform reservation requires pointer-free elements")})
		}
		valueSlots := arrayValueSlots(typeID)
		if len(args) != valueSlots+2 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array check-uniform helper arity = %d, want %d", len(args), valueSlots+2)})
		}
		_ = arrayStoredValue(typeID, args[:valueSlots])
		ref, err := in.gc.ReserveDeadArrayAllocation(in.requireGCDomainType(typeID), uint32(args[valueSlots]), frameRoots)
		if err != nil {
			panicArrayError(err)
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcArrayCheckData:
		if len(args) != 4 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array check-data helper arity = %d, want 4", len(args))})
		}
		source, length := uint32(args[0]), uint32(args[1])
		typeID, dataIndex := uint32(args[2]), uint32(args[3])
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data type %d is not an admitted array", typeID)})
		}
		var width uint64
		switch in.c.GCTypeDescs[typeID].Elem {
		case gc.StorageI8:
			width = 1
		case gc.StorageI16:
			width = 2
		case gc.StorageI32, gc.StorageF32:
			width = 4
		case gc.StorageI64, gc.StorageF64:
			width = 8
		case gc.StorageV128:
			width = 16
		default:
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data type %d has unsupported storage", typeID)})
		}
		if int(dataIndex) >= len(in.c.PassiveData) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data segment %d is unavailable", dataIndex)})
		}
		descOff := int(dataIndex) * coreruntime.PassiveDataDescBytes
		if descOff < 0 || descOff+coreruntime.PassiveDataDescBytes > len(in.passiveDataDesc) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data segment %d has no instance descriptor", dataIndex)})
		}
		segmentLen := binary.LittleEndian.Uint32(in.passiveDataDesc[descOff+8:])
		end := uint64(source) + uint64(length)*width
		if end > uint64(segmentLen) {
			panic(gcStructHelperTrap{code: coreruntime.TrapLinMemOutOfBounds})
		}
		if end > uint64(len(in.c.PassiveData[dataIndex].Bytes)) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data segment %d descriptor length %d exceeds retained bytes %d", dataIndex, segmentLen, len(in.c.PassiveData[dataIndex].Bytes))})
		}
		ref, err := in.gc.ReserveDeadArrayAllocation(in.requireGCDomainType(typeID), length, frameRoots)
		if err != nil {
			panicArrayError(err)
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcArrayAllocFixedV128Spill:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-fixed-spill helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		ptr, count, typeID := uintptr(args[0]), uint32(args[1]), uint32(args[2])
		kind := arrayElemKind(typeID)
		valueSlots := uint64(1)
		if kind == gc.StorageV128 {
			valueSlots = 2
		}
		if count != 0 && ptr == 0 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-fixed-spill has nil source")})
		}
		slotCount := uint64(count) * valueSlots
		if count != 0 && slotCount/valueSlots != uint64(count) || slotCount > uint64(maxInt()/8) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-fixed-spill slot count overflows")})
		}
		var slots []uint64
		if slotCount != 0 {
			slots = unsafe.Slice((*uint64)(offHeapPtr(ptr)), int(slotCount))
		}
		values, valueErr := state.constructorValues(count)
		if valueErr != nil {
			panic(gcStructHelperError{err: valueErr})
		}
		for i := uint32(0); i < count; i++ {
			start := uint64(i) * valueSlots
			values[i] = arrayStoredValue(typeID, slots[start:start+valueSlots])
		}
		ref, err := in.gc.NewArrayFixedWithRoots(in.requireGCDomainType(typeID), values, frameRoots)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = uint64(ref)
	case gcArrayInitElem:
		if len(args) != 6 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array init-elem helper arity = %d, want 6", len(args))})
		}
		ref, dstStart := gc.Ref(uint32(args[0])), uint32(args[1])
		srcStart, length := uint32(args[2]), uint32(args[3])
		typeID, elemIndex := uint32(args[4]), uint32(args[5])
		product := in.c.stagedGCArrayProduct()
		if product != stagedGCArrayProductInitElem && product != stagedGCArrayProductGeneric {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem product %s is unavailable", product)})
		}
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray || !gcArrayElementStorage(in.c.GCTypeDescs[typeID].Elem) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem type %d is not a reference array", typeID)})
		}
		checkArray(ref, typeID)
		entries, segmentLen, entryBytes := arrayElemSegment(elemIndex)
		dstLen, err := in.gc.ArrayLen(ref)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if uint64(dstStart)+uint64(length) > uint64(dstLen) {
			panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
		}
		if uint64(srcStart)+uint64(length) > uint64(segmentLen) {
			panic(gcStructHelperTrap{code: coreruntime.TrapIndirectOutOfBounds})
		}
		// Validate the complete source before publishing the first write so a
		// malformed retained descriptor cannot leave a partially initialized array.
		for i := uint32(0); i < length; i++ {
			_ = arrayElemValue(typeID, entries, entryBytes, srcStart+i)
		}
		deferredBarrier := in.gc.Profile() != gc.ProfileTiny
		for i := uint32(0); i < length; i++ {
			value := arrayElemValuePrevalidated(typeID, entries, entryBytes, srcStart+i)
			var err error
			if deferredBarrier {
				err = in.gc.ArraySetDeferredBarrier(ref, dstStart+i, value)
			} else {
				err = in.gc.ArraySet(ref, dstStart+i, value)
			}
			if err != nil {
				if strings.Contains(err.Error(), "range") {
					panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
				}
				panic(gcStructHelperError{err: err})
			}
		}
		if deferredBarrier {
			in.gc.PostBulkWriteBarrier(ref, dstStart, length)
		}
	case gcArrayInitData:
		if len(args) != 6 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array init-data helper arity = %d, want 6", len(args))})
		}
		ref, dstStart := gc.Ref(uint32(args[0])), uint32(args[1])
		srcStart, length := uint32(args[2]), uint32(args[3])
		typeID, dataIndex := uint32(args[4]), uint32(args[5])
		checkArray(ref, typeID)
		if int(dataIndex) >= len(in.c.PassiveData) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_data segment %d is unavailable", dataIndex)})
		}
		descOff := int(dataIndex) * coreruntime.PassiveDataDescBytes
		if descOff < 0 || descOff+coreruntime.PassiveDataDescBytes > len(in.passiveDataDesc) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_data segment %d has no instance descriptor", dataIndex)})
		}
		segmentLen := binary.LittleEndian.Uint32(in.passiveDataDesc[descOff+8:])
		data := in.c.PassiveData[dataIndex].Bytes
		if segmentLen > uint32(len(data)) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_data segment %d descriptor length %d exceeds retained bytes %d", dataIndex, segmentLen, len(data))})
		}
		if err := in.gc.ArrayInitData(ref, dstStart, data[:segmentLen], srcStart, length); err != nil {
			if strings.Contains(err.Error(), "data source out of range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapLinMemOutOfBounds})
			}
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
			}
			panic(gcStructHelperError{err: err})
		}
	case gcArrayFill:
		if len(args) < 5 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array fill helper arity = %d, want at least 5", len(args))})
		}
		typeID := uint32(args[len(args)-1])
		valueSlots := arrayValueSlots(typeID)
		if len(args) != valueSlots+4 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array fill helper arity = %d, want %d", len(args), valueSlots+4)})
		}
		ref, start := gc.Ref(uint32(args[0])), uint32(args[1])
		checkArray(ref, typeID)
		value := arrayStoredValue(typeID, args[2:2+valueSlots])
		err := in.gc.ArrayFill(ref, start, value, uint32(args[2+valueSlots]))
		if err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
			}
			panic(gcStructHelperError{err: err})
		}
	case gcArrayCopy:
		if len(args) != 7 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array copy helper arity = %d, want 7", len(args))})
		}
		dst, dstStart := gc.Ref(uint32(args[0])), uint32(args[1])
		src, srcStart, length := gc.Ref(uint32(args[2])), uint32(args[3]), uint32(args[4])
		dstType, srcType := uint32(args[5]), uint32(args[6])
		checkArray(dst, dstType)
		checkArray(src, srcType)
		if err := in.gc.ArrayCopy(dst, dstStart, src, srcStart, length); err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
			}
			panic(gcStructHelperError{err: err})
		}
	case gcArrayDropElem:
		if len(args) != 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array elem-drop helper arity = %d, want 1", len(args))})
		}
		elemIndex := uint32(args[0])
		state := in.existingGCArrayElementState()
		if state != nil && elemIndex == 0 {
			state.drop(in.gc)
			break
		}
		if (in.c.stagedGCArrayProduct() == stagedGCArrayProductInitElem || in.c.stagedGCArrayProduct() == stagedGCArrayProductNewElem || in.c.stagedGCArrayProduct() == stagedGCArrayProductGeneric) && int(elemIndex) < len(in.c.passiveElems) && in.jm.PassiveElemPtr() != 0 {
			descOff := int(elemIndex) * coreruntime.PassiveElemDescBytes
			desc := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr()+uintptr(descOff))), coreruntime.PassiveElemDescBytes)
			binary.LittleEndian.PutUint32(desc[8:], 0)
			break
		}
		panic(gcStructHelperError{err: fmt.Errorf("gc array element segment %d is unavailable", elemIndex)})
	case gcArrayAllocElem:
		if len(args) != 4 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-elem helper arity = %d/%d, want 4/at-least-1", len(args), len(results))})
		}
		source, length := uint32(args[0]), uint32(args[1])
		typeID, elemIndex := uint32(args[2]), uint32(args[3])
		if product := in.c.stagedGCArrayProduct(); product == stagedGCArrayProductNewElem || product == stagedGCArrayProductGeneric {
			if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray || !gcArrayElementStorage(in.c.GCTypeDescs[typeID].Elem) {
				panic(gcStructHelperError{err: fmt.Errorf("gc array element type %d is unavailable", typeID)})
			}
			entries, segmentLen, entryBytes := arrayElemSegment(elemIndex)
			if uint64(source)+uint64(length) > uint64(segmentLen) {
				panic(gcStructHelperTrap{code: coreruntime.TrapIndirectOutOfBounds})
			}
			// Validate every source value before allocation. This preserves the
			// all-or-nothing behavior of array.new_elem while allowing non-null
			// reference arrays, which cannot be created through default filling.
			for i := uint32(0); i < length; i++ {
				_ = arrayElemValue(typeID, entries, entryBytes, source+i)
			}
			ref, err := in.gc.NewArrayUninitializedWithRoots(in.requireGCDomainType(typeID), length, frameRoots)
			if err != nil {
				panic(gcStructHelperError{err: err})
			}
			for i := uint32(0); i < length; i++ {
				if err := in.gc.ArraySet(ref, i, arrayElemValue(typeID, entries, entryBytes, source+i)); err != nil {
					panic(gcStructHelperError{err: err})
				}
			}
			results[0] = uint64(ref)
			break
		}
		state := in.existingGCArrayElementState()
		if state == nil || elemIndex != 0 || len(state.Descriptor) < 12 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array element segment %d is unavailable", elemIndex)})
		}
		segmentLen := binary.LittleEndian.Uint32(state.Descriptor[8:])
		end := uint64(source) + uint64(length)
		if end > uint64(segmentLen) || end > uint64(state.Count) {
			panic(gcStructHelperTrap{code: coreruntime.TrapIndirectOutOfBounds})
		}
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray || (in.c.GCTypeDescs[typeID].Elem != gc.StorageRef && in.c.GCTypeDescs[typeID].Elem != gc.StorageRefNull) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_elem type %d is not an admitted reference array", typeID)})
		}
		// The native-frame collection slice rejects element segments, so this
		// product never has non-empty frameRoots. Any future broadening must
		// combine them with these checked segment roots before allocation.
		roots := &state.AllocRoots
		if cap(roots.Values) < int(length) {
			roots.Values = make([]gc.Root, length)
		} else {
			roots.Values = roots.Values[:length]
			clear(roots.Values)
		}
		roots.Count = length
		defer func() {
			clear(roots.Values)
			roots.Count = 0
		}()
		for i := uint32(0); i < roots.Count; i++ {
			rooted, err := in.gc.CheckedTableSlot(state.Slots[source+i])
			if err != nil || rooted.IsNull() {
				panic(gcStructHelperError{err: fmt.Errorf("gc array element root %d is unavailable: %v", source+i, err)})
			}
			roots.Values[i] = gc.Root(rooted)
			_ = arrayRefValue(typeID, uint64(rooted))
		}
		var ref gc.Ref
		var err error
		if length == 0 {
			ref, err = in.gc.NewArrayDefaultWithRoots(in.requireGCDomainType(typeID), 0, roots)
		} else {
			ref, err = in.gc.NewRefArrayWithRoots(in.requireGCDomainType(typeID), length, &roots.Values[0], roots)
		}
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		for i := uint32(0); i < roots.Count; i++ {
			if err := in.gc.ArraySet(ref, i, arrayRefValue(typeID, uint64(roots.ref(i)))); err != nil {
				panic(gcStructHelperError{err: err})
			}
		}
		in.gc.BulkWriteBarrier(ref, 0, length)
		results[0] = uint64(ref)
	case gcArrayAllocData:
		if len(args) != 4 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-data helper arity = %d/%d, want 4/at-least-1", len(args), len(results))})
		}
		source, length := uint32(args[0]), uint32(args[1])
		typeID, dataIndex := uint32(args[2]), uint32(args[3])
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data type %d is not an admitted array", typeID)})
		}
		storage := in.c.GCTypeDescs[typeID].Elem
		width := uint64(0)
		switch storage {
		case gc.StorageI8:
			width = 1
		case gc.StorageI16:
			width = 2
		case gc.StorageI32, gc.StorageF32:
			width = 4
		case gc.StorageI64, gc.StorageF64:
			width = 8
		case gc.StorageV128:
			width = 16
		default:
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data type %d has unsupported storage %d", typeID, storage)})
		}
		if int(dataIndex) >= len(in.c.PassiveData) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data segment %d is unavailable", dataIndex)})
		}
		descOff := int(dataIndex) * coreruntime.PassiveDataDescBytes
		if descOff < 0 || descOff+coreruntime.PassiveDataDescBytes > len(in.passiveDataDesc) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data segment %d has no instance descriptor", dataIndex)})
		}
		segmentLen := binary.LittleEndian.Uint32(in.passiveDataDesc[descOff+8:])
		end := uint64(source) + uint64(length)*width
		if end > uint64(segmentLen) {
			panic(gcStructHelperTrap{code: coreruntime.TrapLinMemOutOfBounds})
		}
		data := in.c.PassiveData[dataIndex].Bytes
		if end > uint64(len(data)) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data segment %d descriptor length %d exceeds retained bytes %d", dataIndex, segmentLen, len(data))})
		}
		ref, err := in.gc.NewArrayDefaultWithRoots(in.requireGCDomainType(typeID), length, frameRoots)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if err := in.gc.ArrayInitData(ref, 0, data[:segmentLen], source, length); err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = uint64(ref)
	case gcArrayAllocUniform, gcArrayAllocUniformNative:
		if len(args) < 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-uniform helper arity = %d/%d, want at least 3/at-least-1", len(args), len(results))})
		}
		typeID := uint32(args[len(args)-1])
		valueSlots := arrayValueSlots(typeID)
		if len(args) != valueSlots+2 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-uniform helper arity = %d, want %d", len(args), valueSlots+2)})
		}
		length := uint32(args[valueSlots])
		value := arrayStoredValue(typeID, args[:valueSlots])
		ref, err := in.gc.NewArrayWithRootScratch(in.requireGCDomainType(typeID), length, value, frameRoots, &state.arrayInitializerRoots)
		if err != nil {
			panicArrayError(err)
		}
		results[0] = uint64(ref)
		if helper == gcArrayAllocUniformNative {
			in.prepareNativeArrayAllocation(typeID, length)
		}
	case gcArrayAllocFixed, gcArrayAllocFixedNative:
		if len(args) < 2 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-fixed helper arity = %d/%d, want at-least-2/at-least-1", len(args), len(results))})
		}
		count := uint32(args[len(args)-1])
		typeID := uint32(args[len(args)-2])
		valueSlots := arrayValueSlots(typeID)
		if uint64(count)*uint64(valueSlots)+2 != uint64(len(args)) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-fixed count = %d, value slots = %d, args = %d", count, valueSlots, len(args))})
		}
		values, valueErr := state.constructorValues(count)
		if valueErr != nil {
			panic(gcStructHelperError{err: valueErr})
		}
		for i := uint32(0); i < count; i++ {
			start := int(i) * valueSlots
			values[i] = arrayStoredValue(typeID, args[start:start+valueSlots])
		}
		ref, err := in.gc.NewArrayFixedPrevalidatedWithRootScratch(in.requireGCDomainType(typeID), values, frameRoots, &state.arrayInitializerRoots)
		if err != nil {
			panicArrayError(err)
		}
		results[0] = uint64(ref)
		if helper == gcArrayAllocFixedNative {
			in.prepareNativeArrayAllocation(typeID, count)
		}
	case gcArrayAllocDefault, gcArrayAllocDefaultNative:
		if len(args) != 2 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-default helper arity = %d/%d, want 2/at-least-1", len(args), len(results))})
		}
		ref, err := in.gc.NewArrayDefaultPrevalidatedWithRoots(in.requireGCDomainType(uint32(args[1])), uint32(args[0]), frameRoots)
		if err != nil {
			panicArrayError(err)
		}
		results[0] = uint64(ref)
		if helper == gcArrayAllocDefaultNative {
			in.prepareNativeArrayAllocation(uint32(args[1]), uint32(args[0]))
		}
	case gcArrayGet, gcArrayGetS, gcArrayGetU:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array get helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		ref, typeID := gc.Ref(uint32(args[0])), uint32(args[2])
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type %d has no Runtime-domain identity", typeID)})
		}
		exact := int(typeID) < len(in.c.Types) && in.c.Types[typeID].Final
		value, actual, matched, err := in.gc.ArrayGetTyped(ref, required, exact, uint32(args[1]))
		if err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
			}
			panic(gcStructHelperError{err: err})
		}
		if !matched {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type = %d, want subtype of %d", actual, typeID)})
		}
		switch helper {
		case gcArrayGetS:
			switch value.Kind {
			case gc.StorageI8:
				results[0] = uint64(uint32(int32(int8(value.Bits))))
			case gc.StorageI16:
				results[0] = uint64(uint32(int32(int16(value.Bits))))
			default:
				panic(gcStructHelperError{err: fmt.Errorf("gc array.get_s element kind %d is not packed", value.Kind)})
			}
		case gcArrayGetU:
			switch value.Kind {
			case gc.StorageI8:
				results[0] = uint64(uint32(uint8(value.Bits)))
			case gc.StorageI16:
				results[0] = uint64(uint32(uint16(value.Bits)))
			default:
				panic(gcStructHelperError{err: fmt.Errorf("gc array.get_u element kind %d is not packed", value.Kind)})
			}
		default:
			if value.Kind == gc.StorageRef || value.Kind == gc.StorageRefNull {
				results[0] = uint64(value.Ref)
			} else {
				results[0] = value.Bits
				if value.Kind == gc.StorageV128 {
					if len(results) < 2 {
						panic(gcStructHelperError{err: fmt.Errorf("gc array get v128 result arity = %d, want at least 2", len(results))})
					}
					results[1] = value.BitsHi
				}
			}
		}
	case gcArraySet:
		if len(args) < 4 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array set helper arity = %d, want at least 4", len(args))})
		}
		typeID := uint32(args[len(args)-1])
		valueSlots := arrayValueSlots(typeID)
		if len(args) != valueSlots+3 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array set helper arity = %d, want %d", len(args), valueSlots+3)})
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type %d has no Runtime-domain identity", typeID)})
		}
		exact := int(typeID) < len(in.c.Types) && in.c.Types[typeID].Final
		value := arrayStoredValue(typeID, args[2:2+valueSlots])
		actual, matched, err := in.gc.ArraySetTyped(ref, required, exact, uint32(args[1]), value)
		if err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
			}
			panic(gcStructHelperError{err: err})
		}
		if !matched {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type = %d, want subtype of %d", actual, typeID)})
		}
	case gcArrayLen:
		if len(args) != 1 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array len helper arity = %d/%d, want 1/at-least-1", len(args), len(results))})
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		length, err := in.gc.ArrayLen(ref)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = uint64(length)
	default:
		panic(gcStructHelperError{err: fmt.Errorf("unknown gc array helper %d", helper)})
	}
}
