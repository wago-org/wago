package wago

import (
	"encoding/binary"
	"errors"
	goruntime "runtime"
	"strconv"
	"strings"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
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
	// 40, 42 and 43 are dead-allocation helpers; 45..47 are RV64 host helpers.
	gcTableFill     uint32 = 48
	gcTableCopy     uint32 = 49
	gcTableGrow     uint32 = 50
	gcTableInit     uint32 = 51
	gcTableElemDrop uint32 = 52
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
	in.dispatchGCHelperParkedOnStack(ctrl, helper, safepoint, args, results, nil)
}

func (in *Instance) dispatchGCHelperParkedOnStack(ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) {
	if in.dispatchCompactExternStore(ctrl, helper, safepoint, args, results, stack) {
		return
	}
	if dispatchOwnedExceptionHelper(in, ctrl, helper, safepoint, args, results, stack) {
		return
	}
	if helper < gcArrayAllocDefault || helper == gcStructReserveDeadNative || helper == gcStructReserveDeadBatch || helper == gcArrayReserveDeadBatch {
		in.dispatchGCStructHelperParked(ctrl, helper, safepoint, args, results, stack)
		return
	}
	in.dispatchGCArrayHelperParked(ctrl, helper, safepoint, args, results, stack)
}

func (in *Instance) dispatchGCArrayHelper(helper uint32, args, results []uint64) {
	in.dispatchGCArrayHelperParked(0, helper, 0, args, results, nil)
}

func (in *Instance) dispatchGCArrayHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) {
	if in == nil || in.gc == nil {
		panic(gcHelperFailuref("gc array helper %d has no live collector", helper))
	}
	defer drainExceptionReclaims(in.gc)
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	var state *gcPublicState
	var frameRoots gc.RootSet = gc.EmptyRoots{}
	if gcHelperMayAllocate(helper) || in.managedFunctionAggregateMayAllocate(helper, args) || in.compactExternPersistentMayAllocate(helper, args) {
		in.gc.CancelNativeAllocationBatch()
		state = in.publicGCState()
		state.mu.Lock()
		defer state.mu.Unlock()
		defer func() { state.frameRoots.physicalStack = nil }()
		frameRoots = in.gcHelperRoots(ctrl, state, safepoint, stack)
	}

	if in.dispatchManagedFunctionAggregate(helper, args, results, frameRoots, state) {
		return
	}

	if in.dispatchCompactExternTableBulk(helper, args, results, lockedDomain, frameRoots) {
		return
	}

	switch helper {
	case gcTableInit:
		if len(args) != 5 {
			panic(gcHelperFailuref("gc table init helper arity = %d, want 5", len(args)))
		}
		descriptor, tableLength, _, exact := in.gcRootedTableDescriptor(args[3])
		destination, source, count := args[0], args[1], args[2]
		if destination+count < destination || destination+count > uint64(tableLength) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		entries, segmentLength := in.gcRootedTableElementSegment(uint32(args[4]), exact)
		if source+count < source || source+count > uint64(segmentLength) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		// Authenticate every retained source root before publishing the first
		// destination write. This keeps trapping/corrupt metadata atomic.
		for i := uint64(0); i < count; i++ {
			word := in.gcRootedTableElementBits(entries, source+i)
			_ = in.gcRootedTableReference(word, exact)
		}
		for i := uint64(0); i < count; i++ {
			ref := in.gcRootedTableReference(in.gcRootedTableElementBits(entries, source+i), exact)
			binary.LittleEndian.PutUint64(descriptor[8+(destination+i)*8:], uint64(ref))
			in.gc.WriteBarrierRoot(ref)
		}
	case gcTableElemDrop:
		if len(args) != 1 {
			panic(gcHelperFailuref("gc table elem-drop helper arity = %d, want 1", len(args)))
		}
		desc, _ := in.gcRootedTableElementDescriptor(uint32(args[0]))
		// Removing the descriptor length under the collector-domain lock makes
		// the passive roots unreachable atomically from the scanner's view.
		binary.LittleEndian.PutUint32(desc[8:], 0)
	case gcTableFill:
		if len(args) != 4 {
			panic(gcHelperFailuref("gc table fill helper arity = %d, want 4", len(args)))
		}
		descriptor, length, _, exact := in.gcRootedTableDescriptor(args[3])
		start, count := args[0], args[2]
		if start+count < start || start+count > uint64(length) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		ref := in.gcRootedTableReference(args[1], exact)
		for i := uint64(0); i < count; i++ {
			binary.LittleEndian.PutUint64(descriptor[8+(start+i)*8:], uint64(ref))
		}
		in.gc.WriteBarrierRoot(ref)
	case gcTableCopy:
		if len(args) != 5 {
			panic(gcHelperFailuref("gc table copy helper arity = %d, want 5", len(args)))
		}
		destination, destinationLength, _, destinationType := in.gcRootedTableDescriptor(args[3])
		source, sourceLength, _, _ := in.gcRootedTableDescriptor(args[4])
		destinationStart, sourceStart, count := args[0], args[1], args[2]
		if destinationStart+count < destinationStart || destinationStart+count > uint64(destinationLength) ||
			sourceStart+count < sourceStart || sourceStart+count > uint64(sourceLength) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		// Validate the full source against the destination's exact table type
		// before the first write. copy then provides memmove semantics for an
		// overlapping same-table range.
		for i := uint64(0); i < count; i++ {
			word := binary.LittleEndian.Uint64(source[8+(sourceStart+i)*8:])
			_ = in.gcRootedTableReference(word, destinationType)
		}
		destinationBytes := destination[8+destinationStart*8 : 8+(destinationStart+count)*8]
		sourceBytes := source[8+sourceStart*8 : 8+(sourceStart+count)*8]
		copy(destinationBytes, sourceBytes)
		for i := uint64(0); i < count; i++ {
			in.gc.WriteBarrierRoot(gc.Ref(uint32(binary.LittleEndian.Uint64(destinationBytes[i*8:]))))
		}
	case gcTableGrow:
		if len(args) != 3 || len(results) < 1 {
			panic(gcHelperFailuref("gc table grow helper arity = %d/%d, want 3/at-least-1", len(args), len(results)))
		}
		descriptor, length, capacity, exact := in.gcRootedTableDescriptor(args[2])
		ref := in.gcRootedTableReference(args[0], exact)
		delta := args[1]
		newLength := uint64(length) + delta
		if newLength < uint64(length) || newLength > uint64(capacity) {
			results[0] = uint64(^uint32(0))
			break
		}
		for i := uint64(length); i < newLength; i++ {
			binary.LittleEndian.PutUint64(descriptor[8+i*8:], uint64(ref))
		}
		in.gc.WriteBarrierRoot(ref)
		// Publish length only after every newly visible root has been initialized.
		binary.LittleEndian.PutUint32(descriptor, uint32(newLength))
		results[0] = uint64(length)
	case gcArrayCheckFixed:
		if len(args) != 2 {
			panic(gcHelperFailuref("gc array check-fixed helper arity = %d, want 2", len(args)))
		}
		ref, err := in.gc.ReserveDeadArrayAllocation(in.requireGCDomainType(uint32(args[0])), uint32(args[1]), frameRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcArrayCheckDefault:
		if len(args) != 2 {
			panic(gcHelperFailuref("gc array check-default helper arity = %d, want 2", len(args)))
		}
		ref, err := in.gc.ReserveDeadDefaultArrayAllocation(in.requireGCDomainType(uint32(args[1])), uint32(args[0]), frameRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcArrayCheckUniform:
		if len(args) < 3 {
			panic(gcHelperFailuref("gc array check-uniform helper arity = %d, want at least 3", len(args)))
		}
		typeID := uint32(args[len(args)-1])
		if gcArrayElementStorage(in.gcArrayElemKind(typeID)) {
			panic(gcHelperFailuref("gc array dead-uniform reservation requires pointer-free elements"))
		}
		valueSlots := in.gcArrayValueSlots(typeID)
		if len(args) != valueSlots+2 {
			panic(gcHelperFailuref("gc array check-uniform helper arity = %d, want %d", len(args), valueSlots+2))
		}
		_ = in.gcArrayStoredValue(typeID, args[:valueSlots])
		ref, err := in.gc.ReserveDeadArrayAllocation(in.requireGCDomainType(typeID), uint32(args[valueSlots]), frameRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcArrayCheckData:
		if len(args) != 4 {
			panic(gcHelperFailuref("gc array check-data helper arity = %d, want 4", len(args)))
		}
		source, length := uint32(args[0]), uint32(args[1])
		typeID, dataIndex := uint32(args[2]), uint32(args[3])
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray {
			panic(gcHelperFailuref("gc array.new_data type %d is not an admitted array", typeID))
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
			panic(gcHelperFailuref("gc array.new_data type %d has unsupported storage", typeID))
		}
		if int(dataIndex) >= len(in.c.PassiveData) {
			panic(gcHelperFailuref("gc array.new_data segment %d is unavailable", dataIndex))
		}
		descOff := int(dataIndex) * coreruntime.PassiveDataDescBytes
		if descOff < 0 || descOff+coreruntime.PassiveDataDescBytes > len(in.passiveDataDesc) {
			panic(gcHelperFailuref("gc array.new_data segment %d has no instance descriptor", dataIndex))
		}
		segmentLen := binary.LittleEndian.Uint32(in.passiveDataDesc[descOff+8:])
		end := uint64(source) + uint64(length)*width
		if end > uint64(segmentLen) {
			panic(gcHelperTrap(coreruntime.TrapLinMemOutOfBounds))
		}
		if end > uint64(len(in.c.PassiveData[dataIndex].Bytes)) {
			panic(gcHelperFailuref("gc array.new_data segment %d descriptor length %d exceeds retained bytes %d", dataIndex, segmentLen, len(in.c.PassiveData[dataIndex].Bytes)))
		}
		ref, err := in.gc.ReserveDeadArrayAllocation(in.requireGCDomainType(typeID), length, frameRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcArrayAllocFixedV128Spill:
		if len(args) != 3 || len(results) < 1 {
			panic(gcHelperFailuref("gc array alloc-fixed-spill helper arity = %d/%d, want 3/at-least-1", len(args), len(results)))
		}
		ptr, count, typeID := uintptr(args[0]), uint32(args[1]), uint32(args[2])
		kind := in.gcArrayElemKind(typeID)
		valueSlots := uint64(1)
		if kind == gc.StorageV128 {
			valueSlots = 2
		}
		if count != 0 && ptr == 0 {
			panic(gcHelperFailuref("gc array alloc-fixed-spill has nil source"))
		}
		slotCount := uint64(count) * valueSlots
		if count != 0 && slotCount/valueSlots != uint64(count) || slotCount > uint64(maxInt()/8) {
			panic(gcHelperFailuref("gc array alloc-fixed-spill slot count overflows"))
		}
		var slots []uint64
		if slotCount != 0 {
			slots = unsafe.Slice((*uint64)(offHeapPtr(ptr)), int(slotCount))
		}
		values, valueErr := state.constructorValues(count)
		if valueErr != nil {
			panic(gcHelperFailure(valueErr))
		}
		for i := uint32(0); i < count; i++ {
			start := uint64(i) * valueSlots
			values[i] = in.gcArrayStoredValue(typeID, slots[start:start+valueSlots])
		}
		ref, err := in.gc.NewArrayFixedWithRoots(in.requireGCDomainType(typeID), values, frameRoots)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		results[0] = uint64(ref)
	case gcArrayInitElem:
		if len(args) != 6 {
			panic(gcHelperFailuref("gc array init-elem helper arity = %d, want 6", len(args)))
		}
		ref, dstStart := gc.Ref(uint32(args[0])), uint32(args[1])
		srcStart, length := uint32(args[2]), uint32(args[3])
		typeID, elemIndex := uint32(args[4]), uint32(args[5])
		product := in.c.stagedGCArrayProduct()
		if product != stagedGCArrayProductInitElem && product != stagedGCArrayProductGeneric {
			panic(gcHelperFailuref("gc array.init_elem product %s is unavailable", product))
		}
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray || !gcArrayElementStorage(in.c.GCTypeDescs[typeID].Elem) {
			panic(gcHelperFailuref("gc array.init_elem type %d is not a reference array", typeID))
		}
		in.gcCheckArray(ref, typeID)
		entries, segmentLen, entryBytes := in.gcArrayElemSegment(elemIndex)
		dstLen, err := in.gc.ArrayLen(ref)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if uint64(dstStart)+uint64(length) > uint64(dstLen) {
			panic(gcHelperTrap(coreruntime.TrapBuiltin))
		}
		if uint64(srcStart)+uint64(length) > uint64(segmentLen) {
			panic(gcHelperTrap(coreruntime.TrapIndirectOutOfBounds))
		}
		// Validate the complete source before publishing the first write so a
		// malformed retained descriptor cannot leave a partially initialized array.
		for i := uint32(0); i < length; i++ {
			_ = in.gcArrayElemValue(typeID, entries, entryBytes, srcStart+i)
		}
		deferredBarrier := in.gc.Profile() != gc.ProfileTiny
		for i := uint32(0); i < length; i++ {
			value := in.gcArrayElemValuePrevalidated(typeID, entries, entryBytes, srcStart+i)
			var err error
			if deferredBarrier {
				err = in.gc.ArraySetDeferredBarrier(ref, dstStart+i, value)
			} else {
				err = in.gc.ArraySet(ref, dstStart+i, value)
			}
			if err != nil {
				if strings.Contains(err.Error(), "range") {
					panic(gcHelperTrap(coreruntime.TrapBuiltin))
				}
				panic(gcHelperFailure(err))
			}
		}
		if deferredBarrier {
			in.gc.PostBulkWriteBarrier(ref, dstStart, length)
		}
	case gcArrayInitData:
		if len(args) != 6 {
			panic(gcHelperFailuref("gc array init-data helper arity = %d, want 6", len(args)))
		}
		ref, dstStart := gc.Ref(uint32(args[0])), uint32(args[1])
		srcStart, length := uint32(args[2]), uint32(args[3])
		typeID, dataIndex := uint32(args[4]), uint32(args[5])
		in.gcCheckArray(ref, typeID)
		if int(dataIndex) >= len(in.c.PassiveData) {
			panic(gcHelperFailuref("gc array.init_data segment %d is unavailable", dataIndex))
		}
		descOff := int(dataIndex) * coreruntime.PassiveDataDescBytes
		if descOff < 0 || descOff+coreruntime.PassiveDataDescBytes > len(in.passiveDataDesc) {
			panic(gcHelperFailuref("gc array.init_data segment %d has no instance descriptor", dataIndex))
		}
		segmentLen := binary.LittleEndian.Uint32(in.passiveDataDesc[descOff+8:])
		data := in.c.PassiveData[dataIndex].Bytes
		if segmentLen > uint32(len(data)) {
			panic(gcHelperFailuref("gc array.init_data segment %d descriptor length %d exceeds retained bytes %d", dataIndex, segmentLen, len(data)))
		}
		if err := in.gc.ArrayInitData(ref, dstStart, data[:segmentLen], srcStart, length); err != nil {
			if strings.Contains(err.Error(), "data source out of range") {
				panic(gcHelperTrap(coreruntime.TrapLinMemOutOfBounds))
			}
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcHelperTrap(coreruntime.TrapBuiltin))
			}
			panic(gcHelperFailure(err))
		}
	case gcArrayFill:
		if len(args) < 5 {
			panic(gcHelperFailuref("gc array fill helper arity = %d, want at least 5", len(args)))
		}
		typeID := uint32(args[len(args)-1])
		valueSlots := in.gcArrayValueSlots(typeID)
		if len(args) != valueSlots+4 {
			panic(gcHelperFailuref("gc array fill helper arity = %d, want %d", len(args), valueSlots+4))
		}
		ref, start := gc.Ref(uint32(args[0])), uint32(args[1])
		in.gcCheckArray(ref, typeID)
		value := in.gcArrayStoredValue(typeID, args[2:2+valueSlots])
		err := in.gc.ArrayFill(ref, start, value, uint32(args[2+valueSlots]))
		if err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcHelperTrap(coreruntime.TrapBuiltin))
			}
			panic(gcHelperFailure(err))
		}
	case gcArrayCopy:
		if len(args) != 7 {
			panic(gcHelperFailuref("gc array copy helper arity = %d, want 7", len(args)))
		}
		dst, dstStart := gc.Ref(uint32(args[0])), uint32(args[1])
		src, srcStart, length := gc.Ref(uint32(args[2])), uint32(args[3]), uint32(args[4])
		dstType, srcType := uint32(args[5]), uint32(args[6])
		in.gcCheckArray(dst, dstType)
		in.gcCheckArray(src, srcType)
		if err := in.gc.ArrayCopy(dst, dstStart, src, srcStart, length); err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcHelperTrap(coreruntime.TrapBuiltin))
			}
			panic(gcHelperFailure(err))
		}
	case gcArrayDropElem:
		if len(args) != 1 {
			panic(gcHelperFailuref("gc array elem-drop helper arity = %d, want 1", len(args)))
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
		panic(gcHelperFailuref("gc array element segment %d is unavailable", elemIndex))
	case gcArrayAllocElem:
		if len(args) != 4 || len(results) < 1 {
			panic(gcHelperFailuref("gc array alloc-elem helper arity = %d/%d, want 4/at-least-1", len(args), len(results)))
		}
		source, length := uint32(args[0]), uint32(args[1])
		typeID, elemIndex := uint32(args[2]), uint32(args[3])
		if product := in.c.stagedGCArrayProduct(); !rv64GCArrayReference(in.c) && (product == stagedGCArrayProductNewElem || product == stagedGCArrayProductGeneric) {
			if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray || !gcArrayElementStorage(in.c.GCTypeDescs[typeID].Elem) {
				panic(gcHelperFailuref("gc array element type %d is unavailable", typeID))
			}
			entries, segmentLen, entryBytes := in.gcArrayElemSegment(elemIndex)
			if uint64(source)+uint64(length) > uint64(segmentLen) {
				panic(gcHelperTrap(coreruntime.TrapIndirectOutOfBounds))
			}
			// Validate every source value before allocation. This preserves the
			// all-or-nothing behavior of array.new_elem while allowing non-null
			// reference arrays, which cannot be created through default filling.
			for i := uint32(0); i < length; i++ {
				_ = in.gcArrayElemValue(typeID, entries, entryBytes, source+i)
			}
			ref, err := in.gc.NewArrayUninitializedWithRoots(in.requireGCDomainType(typeID), length, frameRoots)
			if err != nil {
				panic(gcHelperFailure(err))
			}
			for i := uint32(0); i < length; i++ {
				if err := in.gc.ArraySet(ref, i, in.gcArrayElemValue(typeID, entries, entryBytes, source+i)); err != nil {
					panic(gcHelperFailure(err))
				}
			}
			results[0] = uint64(ref)
			break
		}
		state := in.existingGCArrayElementState()
		if state == nil || elemIndex != 0 || len(state.Descriptor) < 12 {
			panic(gcHelperFailuref("gc array element segment %d is unavailable", elemIndex))
		}
		segmentLen := binary.LittleEndian.Uint32(state.Descriptor[8:])
		end := uint64(source) + uint64(length)
		if end > uint64(segmentLen) || end > uint64(state.Count) {
			panic(gcHelperTrap(coreruntime.TrapIndirectOutOfBounds))
		}
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray || (in.c.GCTypeDescs[typeID].Elem != gc.StorageRef && in.c.GCTypeDescs[typeID].Elem != gc.StorageRefNull) {
			panic(gcHelperFailuref("gc array.new_elem type %d is not an admitted reference array", typeID))
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
				panic(gcHelperFailuref("gc array element root %d is unavailable: %v", source+i, err))
			}
			roots.Values[i] = gc.Root(rooted)
			_ = in.gcArrayRefValue(typeID, uint64(rooted))
		}
		var ref gc.Ref
		var err error
		if length == 0 {
			ref, err = in.gc.NewArrayDefaultWithRoots(in.requireGCDomainType(typeID), 0, roots)
		} else {
			ref, err = in.gc.NewRefArrayWithRoots(in.requireGCDomainType(typeID), length, &roots.Values[0], roots)
		}
		if err != nil {
			panic(gcHelperFailure(err))
		}
		for i := uint32(0); i < roots.Count; i++ {
			if err := in.gc.ArraySet(ref, i, in.gcArrayRefValue(typeID, uint64(roots.ref(i)))); err != nil {
				panic(gcHelperFailure(err))
			}
		}
		in.gc.BulkWriteBarrier(ref, 0, length)
		results[0] = uint64(ref)
	case gcArrayAllocData:
		if len(args) != 4 || len(results) < 1 {
			panic(gcHelperFailuref("gc array alloc-data helper arity = %d/%d, want 4/at-least-1", len(args), len(results)))
		}
		source, length := uint32(args[0]), uint32(args[1])
		typeID, dataIndex := uint32(args[2]), uint32(args[3])
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray {
			panic(gcHelperFailuref("gc array.new_data type %d is not an admitted array", typeID))
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
			panic(gcHelperFailuref("gc array.new_data type %d has unsupported storage %d", typeID, storage))
		}
		if int(dataIndex) >= len(in.c.PassiveData) {
			panic(gcHelperFailuref("gc array.new_data segment %d is unavailable", dataIndex))
		}
		descOff := int(dataIndex) * coreruntime.PassiveDataDescBytes
		if descOff < 0 || descOff+coreruntime.PassiveDataDescBytes > len(in.passiveDataDesc) {
			panic(gcHelperFailuref("gc array.new_data segment %d has no instance descriptor", dataIndex))
		}
		segmentLen := binary.LittleEndian.Uint32(in.passiveDataDesc[descOff+8:])
		end := uint64(source) + uint64(length)*width
		if end > uint64(segmentLen) {
			panic(gcHelperTrap(coreruntime.TrapLinMemOutOfBounds))
		}
		data := in.c.PassiveData[dataIndex].Bytes
		if end > uint64(len(data)) {
			panic(gcHelperFailuref("gc array.new_data segment %d descriptor length %d exceeds retained bytes %d", dataIndex, segmentLen, len(data)))
		}
		var ref gc.Ref
		var err error
		if goruntime.GOARCH == "amd64" {
			ref, err = in.gc.NewArrayDataWithRoots(in.requireGCDomainType(typeID), length, data[:segmentLen], source, frameRoots)
		} else {
			ref, err = in.gc.NewArrayDefaultWithRoots(in.requireGCDomainType(typeID), length, frameRoots)
			if err == nil {
				err = in.gc.ArrayInitData(ref, 0, data[:segmentLen], source, length)
			}
		}
		if err != nil {
			panic(gcHelperFailure(err))
		}
		results[0] = uint64(ref)
	case gcArrayAllocUniform, gcArrayAllocUniformNative:
		if len(args) < 3 || len(results) < 1 {
			panic(gcHelperFailuref("gc array alloc-uniform helper arity = %d/%d, want at least 3/at-least-1", len(args), len(results)))
		}
		typeID := uint32(args[len(args)-1])
		valueSlots := in.gcArrayValueSlots(typeID)
		if len(args) != valueSlots+2 {
			panic(gcHelperFailuref("gc array alloc-uniform helper arity = %d, want %d", len(args), valueSlots+2))
		}
		length := uint32(args[valueSlots])
		value := in.gcArrayStoredValue(typeID, args[:valueSlots])
		ref, err := in.gc.NewArrayWithRootScratch(in.requireGCDomainType(typeID), length, value, frameRoots, &state.arrayInitializerRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		results[0] = uint64(ref)
		if helper == gcArrayAllocUniformNative {
			in.prepareNativeArrayAllocation(typeID, length)
		}
	case gcArrayAllocFixed, gcArrayAllocFixedNative:
		if len(args) < 2 || len(results) < 1 {
			panic(gcHelperFailuref("gc array alloc-fixed helper arity = %d/%d, want at-least-2/at-least-1", len(args), len(results)))
		}
		count := uint32(args[len(args)-1])
		typeID := uint32(args[len(args)-2])
		valueSlots := in.gcArrayValueSlots(typeID)
		if uint64(count)*uint64(valueSlots)+2 != uint64(len(args)) {
			panic(gcHelperFailuref("gc array alloc-fixed count = %d, value slots = %d, args = %d", count, valueSlots, len(args)))
		}
		values, valueErr := state.constructorValues(count)
		if valueErr != nil {
			panic(gcHelperFailure(valueErr))
		}
		for i := uint32(0); i < count; i++ {
			start := int(i) * valueSlots
			values[i] = in.gcArrayStoredValue(typeID, args[start:start+valueSlots])
		}
		ref, err := in.gc.NewArrayFixedPrevalidatedWithRootScratch(in.requireGCDomainType(typeID), values, frameRoots, &state.arrayInitializerRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		results[0] = uint64(ref)
		if helper == gcArrayAllocFixedNative {
			in.prepareNativeArrayAllocation(typeID, count)
		}
	case gcArrayAllocDefault, gcArrayAllocDefaultNative:
		if len(args) != 2 || len(results) < 1 {
			panic(gcHelperFailuref("gc array alloc-default helper arity = %d/%d, want 2/at-least-1", len(args), len(results)))
		}
		ref, err := in.gc.NewArrayDefaultPrevalidatedWithRoots(in.requireGCDomainType(uint32(args[1])), uint32(args[0]), frameRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		results[0] = uint64(ref)
		if helper == gcArrayAllocDefaultNative {
			in.prepareNativeDefaultArrayAllocation(uint32(args[1]), uint32(args[0]))
		}
	case gcArrayGet, gcArrayGetS, gcArrayGetU:
		if len(args) != 3 || len(results) < 1 {
			panic(gcHelperFailuref("gc array get helper arity = %d/%d, want 3/at-least-1", len(args), len(results)))
		}
		ref, typeID := gc.Ref(uint32(args[0])), uint32(args[2])
		if ref.IsNull() {
			panic(gcHelperTrap(coreruntime.TrapNullReference))
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcHelperFailuref("gc array type %d has no Runtime-domain identity", typeID))
		}
		exact := int(typeID) < len(in.c.Types) && in.c.Types[typeID].Final
		value, actual, matched, err := in.gc.ArrayGetTyped(ref, required, exact, uint32(args[1]))
		if err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcHelperTrap(coreruntime.TrapBuiltin))
			}
			panic(gcHelperFailure(err))
		}
		if !matched {
			panic(gcHelperFailuref("gc array type = %d, want subtype of %d", actual, typeID))
		}
		switch helper {
		case gcArrayGetS:
			switch value.Kind {
			case gc.StorageI8:
				results[0] = uint64(uint32(int32(int8(value.Bits))))
			case gc.StorageI16:
				results[0] = uint64(uint32(int32(int16(value.Bits))))
			default:
				panic(gcHelperFailuref("gc array.get_s element kind %d is not packed", value.Kind))
			}
		case gcArrayGetU:
			switch value.Kind {
			case gc.StorageI8:
				results[0] = uint64(uint32(uint8(value.Bits)))
			case gc.StorageI16:
				results[0] = uint64(uint32(uint16(value.Bits)))
			default:
				panic(gcHelperFailuref("gc array.get_u element kind %d is not packed", value.Kind))
			}
		default:
			if value.Kind == gc.StorageRef || value.Kind == gc.StorageRefNull {
				results[0] = uint64(value.Ref)
			} else {
				results[0] = value.Bits
				if value.Kind == gc.StorageV128 {
					if len(results) < 2 {
						panic(gcHelperFailuref("gc array get v128 result arity = %d, want at least 2", len(results)))
					}
					results[1] = value.BitsHi
				}
			}
		}
	case gcArraySet:
		if len(args) < 4 {
			panic(gcHelperFailuref("gc array set helper arity = %d, want at least 4", len(args)))
		}
		typeID := uint32(args[len(args)-1])
		valueSlots := in.gcArrayValueSlots(typeID)
		if len(args) != valueSlots+3 {
			panic(gcHelperFailuref("gc array set helper arity = %d, want %d", len(args), valueSlots+3))
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcHelperTrap(coreruntime.TrapNullReference))
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcHelperFailuref("gc array type %d has no Runtime-domain identity", typeID))
		}
		exact := int(typeID) < len(in.c.Types) && in.c.Types[typeID].Final
		value := in.gcArrayStoredValue(typeID, args[2:2+valueSlots])
		actual, matched, err := in.gc.ArraySetTyped(ref, required, exact, uint32(args[1]), value)
		if err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcHelperTrap(coreruntime.TrapBuiltin))
			}
			panic(gcHelperFailure(err))
		}
		if !matched {
			panic(gcHelperFailuref("gc array type = %d, want subtype of %d", actual, typeID))
		}
	case gcArrayLen:
		if len(args) != 1 || len(results) < 1 {
			panic(gcHelperFailuref("gc array len helper arity = %d/%d, want 1/at-least-1", len(args), len(results)))
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcHelperTrap(coreruntime.TrapNullReference))
		}
		length, err := in.gc.ArrayLen(ref)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		results[0] = uint64(length)
	default:
		panic(gcHelperFailuref("unknown gc array helper %d", helper))
	}
}

// gcRootedTableDescriptor returns a bounded compact collector table.
// Generic rooted modules authenticate this shape in their retained receipt;
// these checks keep a corrupted runtime descriptor from becoming an arbitrary
// off-heap write primitive.
func (in *Instance) gcRootedTableDescriptor(table uint64) ([]byte, uint32, uint32, ValueTypeDescriptor) {
	descriptor, length, capacity, exact, _ := in.gcRootedTableStorage(table)
	return descriptor, length, capacity, exact
}

func (in *Instance) gcRootedTableStorage(table uint64) ([]byte, uint32, uint32, ValueTypeDescriptor, *Instance) {
	d, n, capacity, typ, owner, _ := in.gcRootedTablePhysicalStorage(table)
	return d, n, capacity, typ, owner
}
func (in *Instance) gcRootedTablePhysicalStorage(table uint64) ([]byte, uint32, uint32, ValueTypeDescriptor, *Instance, *tableOwner) {
	var hostOwner *tableOwner

	storageOwner := in
	if table >= uint64(in.c.tableCount()) || in.existingGCRefTestTableState() != nil {
		panic(gcHelperFailuref("generic GC table %d is unavailable", table))
	}
	index := int(table)
	opaque := in.c.compactExternFrameRoots() && in.c.tableElementType(index) == ValExternRef
	if in.c.tableEntryBytes(index) != 8 || !isGCRefValType(in.c.tableElementType(index)) && !opaque {
		panic(gcHelperFailuref("generic GC table %d is not a compact collector table", table))
	}
	exact, err := in.c.tableExactType(index)
	if err != nil || exact.Kind != ValueTypeReference || !exact.Ref.Nullable || exact.Ref.Exact {
		panic(gcHelperFailuref("generic GC table %d has no collector reference type", table))
	}
	if exact.Ref.Heap.Defined {
		if exact.Ref.Heap.Abstract != 0 || int(exact.Ref.Heap.TypeIndex) >= len(in.c.Types) {
			panic(gcHelperFailuref("generic GC table %d has invalid aggregate reference type", table))
		}
		kind := in.c.Types[exact.Ref.Heap.TypeIndex].Kind
		if kind != CompositeTypeStruct && kind != CompositeTypeArray {
			panic(gcHelperFailuref("generic GC table %d has non-aggregate reference type", table))
		}
	} else {
		if exact.Ref.Heap.TypeIndex != 0 {
			panic(gcHelperFailuref("generic GC table %d has invalid abstract reference type", table))
		}
		switch exact.Ref.Heap.Abstract {
		case AbstractHeapExtern, AbstractHeapNoExtern:
			if !opaque {
				panic(gcHelperFailuref("generic GC table has unqualified extern storage"))
			}
		case AbstractHeapAny, AbstractHeapEq, AbstractHeapI31, AbstractHeapStruct, AbstractHeapArray, AbstractHeapNone:
		default:
			panic(gcHelperFailuref("generic GC table %d has unsupported abstract reference type", table))
		}
	}
	var descriptor []byte
	capacity := in.c.tableRuntimeCapacity(index)
	if def, imported := in.c.tableImportAt(index); imported {
		_, ends, _, _, _ := in.c.importModuleEndSections()
		module, name := splitImportKeyAt(def.Key, importModuleEndAt(ends, index))
		// The map consumes this key only for the lookup. Keep ordinary names on
		// the stack instead of allocating a returned binding-key string.
		var keyBuffer [256]byte
		key := strconv.AppendInt(keyBuffer[:0], int64(len(module)), 10)
		key = append(key, ':')
		key = append(key, module...)
		key = append(key, name...)
		shared, ok := in.imports[string(key)].(*Table)
		if !ok || shared.owner == nil {
			panic(gcHelperFailuref("generic GC table %d has no retained owner", table))
		}
		owner := shared.owner
		owner.mu.Lock()
		valid := !owner.closed && owner.store != nil && owner.store == in.refStore && (owner.instance != nil && owner.instance.gc == in.gc || opaque && owner.instance == nil && railshotCompactExternHostTableMatches(shared, in.refStore, in.gc))
		if valid && owner.instance == nil {
			hostOwner = owner
		}
		storageOwner = owner.instance
		owner.mu.Unlock()
		if !valid {
			panic(gcHelperFailuref("generic GC table %d belongs to an incompatible collector domain", table))
		}
		descriptor = shared.desc
		// Import limits describe the accepted external type. The retained owner
		// supplies the actual storage capacity, which can be smaller.
		if len(descriptor) < 8 || (len(descriptor)-8)%8 != 0 {
			panic(gcHelperFailuref("generic GC table %d descriptor size is invalid", table))
		}
		capacity = (len(descriptor) - 8) / 8
	} else {
		descriptor = gcRetainedTableDescriptor(in, index)
	}
	if capacity < 0 || capacity > int(^uint32(0)) || len(descriptor) != 8+capacity*8 {
		panic(gcHelperFailuref("generic GC table %d descriptor size is invalid", table))
	}
	length, runtimeCapacity := binary.LittleEndian.Uint32(descriptor), binary.LittleEndian.Uint32(descriptor[4:])
	if runtimeCapacity != uint32(capacity) || length > runtimeCapacity {
		panic(gcHelperFailuref("generic GC table %d descriptor header is invalid", table))
	}
	return descriptor, length, runtimeCapacity, exact, storageOwner, hostOwner
}

func (in *Instance) gcRootedTableReference(word uint64, exact ValueTypeDescriptor) gc.Ref {
	ref := gc.Ref(uint32(word))
	if word != uint64(ref) {
		panic(gcHelperFailuref("generic GC table mutation contains non-compact reference %#x", word))
	}
	if ref.IsNull() {
		return ref
	}
	if !in.gcRefMatchesValueType(ref, exact) {
		panic(gcHelperFailuref("generic GC table mutation contains an incompatible reference"))
	}
	return ref
}

func (in *Instance) gcRootedTableElementDescriptor(elemIndex uint32) ([]byte, ElemInit) {
	if int(elemIndex) >= len(in.c.passiveElems) || in.jm.PassiveElemPtr() == 0 {
		panic(gcHelperFailuref("generic GC table element segment %d is unavailable", elemIndex))
	}
	elem := in.c.passiveElems[elemIndex]
	if elem.Mode != ElemModePassive && (elem.Mode != ElemModeActive && elem.Mode != ElemModeDeclarative || len(elem.Values) != 0) || elemEntryBytes(elem.RefType) != 8 || !isGCRefValType(normalizedElemRefType(elem.RefType)) && (!in.c.compactExternFrameRoots() || normalizedElemRefType(elem.RefType) != ValExternRef) {
		panic(gcHelperFailuref("generic GC table element segment %d is not passive aggregate reference data", elemIndex))
	}
	descOff := int(elemIndex) * coreruntime.PassiveElemDescBytes
	desc := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr()+uintptr(descOff))), coreruntime.PassiveElemDescBytes)
	return desc, elem
}

func (in *Instance) gcRootedTableElementSegment(elemIndex uint32, tableType ValueTypeDescriptor) (uintptr, uint32) {
	desc, elem := in.gcRootedTableElementDescriptor(elemIndex)
	exact, err := in.c.elemExactType(elem)
	if err != nil || !valueTypeSubtype(exact, in.c.Types, tableType, in.c.Types) {
		panic(gcHelperFailuref("generic GC table element segment %d type does not match destination", elemIndex))
	}
	length := binary.LittleEndian.Uint32(desc[8:])
	if uint64(length) > uint64(len(elem.Values)) {
		panic(gcHelperFailuref("generic GC table element segment %d length %d exceeds retained entries %d", elemIndex, length, len(elem.Values)))
	}
	entries := uintptr(binary.LittleEndian.Uint64(desc))
	if length != 0 && entries == 0 {
		panic(gcHelperFailuref("generic GC table element segment %d has no entries", elemIndex))
	}
	return entries, length
}

func (in *Instance) gcRootedTableElementBits(entries uintptr, index uint64) uint64 {
	if index > uint64(^uintptr(0))/8 || uint64(entries) > uint64(^uintptr(0))-index*8 {
		panic(gcHelperFailuref("generic GC table element entry %d address overflows", index))
	}
	entry := unsafe.Slice((*byte)(offHeapPtr(entries+uintptr(index*8))), 8)
	return binary.LittleEndian.Uint64(entry)
}

//go:noinline
func (in *Instance) gcCheckArray(ref gc.Ref, typeID uint32) {
	if ref.IsNull() {
		panic(gcHelperTrap(coreruntime.TrapNullReference))
	}
	actual, err := in.gc.ObjectType(ref)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	if !in.gcObjectTypeMatches(actual, typeID) {
		panic(gcHelperFailuref("gc array type = %d, want subtype of %d", actual, typeID))
	}
}

//go:noinline
func (in *Instance) gcArrayElemKind(typeID uint32) gc.StorageKind {
	if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray {
		panic(gcHelperFailuref("gc array type %d is unavailable", typeID))
	}
	return in.c.GCTypeDescs[typeID].Elem
}

//go:noinline
func (in *Instance) gcArrayValueSlots(typeID uint32) int {
	if in.gcArrayElemKind(typeID) == gc.StorageV128 {
		return 2
	}
	return 1
}

//go:noinline
func (in *Instance) gcArrayValue(typeID uint32, words []uint64) gc.Value {
	kind := in.gcArrayElemKind(typeID)
	if kind == gc.StorageRef || kind == gc.StorageRefNull {
		panic(gcHelperFailuref("gc array reference elements remain outside the staged helper slice"))
	}
	wantSlots := 1
	if kind == gc.StorageV128 {
		wantSlots = 2
	}
	if len(words) != wantSlots {
		panic(gcHelperFailuref("gc array type %d value uses %d slot(s), want %d", typeID, len(words), wantSlots))
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

//go:noinline
func (in *Instance) gcArrayRefValue(typeID uint32, bits uint64) gc.Value {
	if int(typeID) >= len(in.c.Types) || in.c.Types[typeID].Kind != CompositeTypeArray || in.c.Types[typeID].Array.Storage.Value.Kind != ValueTypeReference {
		panic(gcHelperFailuref("gc array type %d has no reference element descriptor", typeID))
	}
	ref := gc.Ref(uint32(bits))
	want := in.c.Types[typeID].Array.Storage.Value
	if ref.IsNull() {
		if !want.Ref.Nullable {
			panic(gcHelperFailuref("gc array type %d rejects null reference element", typeID))
		}
		return gc.RefValue(ref)
	}
	if !in.gcRefMatchesValueType(ref, want) {
		panic(gcHelperFailuref("gc array reference does not match destination type %d", typeID))
	}
	return gc.RefValue(ref)
}

//go:noinline
func (in *Instance) gcArrayStoredValue(typeID uint32, words []uint64) gc.Value {
	kind := in.gcArrayElemKind(typeID)
	if kind == gc.StorageRef || kind == gc.StorageRefNull {
		if len(words) != 1 {
			panic(gcHelperFailuref("gc array reference value uses %d slots", len(words)))
		}
		return in.gcArrayRefValue(typeID, words[0])
	}
	return in.gcArrayValue(typeID, words)
}

//go:noinline
func (in *Instance) gcArrayElemSegment(elemIndex uint32) (entries uintptr, length uint32, entryBytes int) {
	if int(elemIndex) >= len(in.c.passiveElems) || in.jm.PassiveElemPtr() == 0 {
		panic(gcHelperFailuref("gc array element segment %d is unavailable", elemIndex))
	}
	descOff := int(elemIndex) * coreruntime.PassiveElemDescBytes
	desc := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr()+uintptr(descOff))), coreruntime.PassiveElemDescBytes)
	length = binary.LittleEndian.Uint32(desc[8:])
	if uint64(length) > uint64(len(in.c.passiveElems[elemIndex].Values)) {
		panic(gcHelperFailuref("gc array element segment %d length %d exceeds retained entries %d", elemIndex, length, len(in.c.passiveElems[elemIndex].Values)))
	}
	entries = uintptr(binary.LittleEndian.Uint64(desc))
	if length != 0 && entries == 0 {
		panic(gcHelperFailuref("gc array element segment %d has no entries", elemIndex))
	}
	entryBytes = elemEntryBytes(in.c.passiveElems[elemIndex].RefType)
	return
}

//go:noinline
func (in *Instance) gcArrayElemBits(entries uintptr, entryBytes int, index uint32) uint64 {
	off := uint64(index) * uint64(entryBytes)
	if off > uint64(^uintptr(0))-uint64(entries) {
		panic(gcHelperFailuref("gc array element entry %d address overflows", index))
	}
	entry := unsafe.Slice((*byte)(offHeapPtr(entries+uintptr(off))), entryBytes)
	if entryBytes == coreruntime.TableEntryBytes {
		return binary.LittleEndian.Uint64(entry[coreruntime.TableEntryRefSlotOffset:])
	}
	return binary.LittleEndian.Uint64(entry)
}

//go:noinline
func (in *Instance) gcArrayElemValue(typeID uint32, entries uintptr, entryBytes int, index uint32) gc.Value {
	return in.gcArrayStoredValue(typeID, []uint64{in.gcArrayElemBits(entries, entryBytes, index)})
}

//go:noinline
func (in *Instance) gcArrayElemValuePrevalidated(typeID uint32, entries uintptr, entryBytes int, index uint32) gc.Value {
	bits := in.gcArrayElemBits(entries, entryBytes, index)
	kind := in.gcArrayElemKind(typeID)
	if kind == gc.StorageRef || kind == gc.StorageRefNull {
		return gc.RefValue(gc.Ref(uint32(bits)))
	}
	return gc.Value{Kind: kind, Bits: bits}
}

//go:noinline
func (in *Instance) gcPanicArrayError(err error) {
	if errors.Is(err, gc.ErrAllocationTooLarge) {
		panic(gcHelperTrap(coreruntime.TrapBuiltin))
	}
	panic(gcHelperFailure(err))
}
