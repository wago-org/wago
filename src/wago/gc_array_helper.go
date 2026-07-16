package wago

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const (
	gcArrayAllocDefault uint32 = 16
	gcArrayGet          uint32 = 17
	gcArrayGetS         uint32 = 18
	gcArrayGetU         uint32 = 19
	gcArraySet          uint32 = 20
	gcArrayLen          uint32 = 21
	gcArrayAllocFixed   uint32 = 22
	gcArrayAllocUniform uint32 = 23
	gcArrayAllocData    uint32 = 24
	gcArrayAllocElem    uint32 = 25
	gcArrayDropElem     uint32 = 26
	gcArrayFill         uint32 = 27
	gcArrayCopy         uint32 = 28
	gcArrayInitData     uint32 = 29
	gcArrayInitElem     uint32 = 30
)

func (in *Instance) dispatchGCHelper(helper uint32, args, results []uint64) {
	if helper < gcArrayAllocDefault {
		in.dispatchGCStructHelper(helper, args, results)
		return
	}
	in.dispatchGCArrayHelper(helper, args, results)
}

func (in *Instance) dispatchGCArrayHelper(helper uint32, args, results []uint64) {
	if in == nil || in.gc == nil {
		panic(gcStructHelperError{err: fmt.Errorf("gc array helper %d has no live collector", helper)})
	}
	state := in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()

	checkArray := func(ref gc.Ref, typeID uint32) {
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		actual, err := in.gc.ObjectType(ref)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if actual != gc.TypeID(typeID) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type = %d, want %d", actual, typeID)})
		}
	}
	arrayValue := func(typeID uint32, bits uint64) gc.Value {
		if int(typeID) >= len(in.c.GCTypeDescs) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array type %d is unavailable", typeID)})
		}
		kind := in.c.GCTypeDescs[typeID].Elem
		if kind == gc.StorageRef || kind == gc.StorageRefNull {
			panic(gcStructHelperError{err: fmt.Errorf("gc array reference elements remain outside the staged helper slice")})
		}
		valueKind := kind
		if kind == gc.StorageI8 || kind == gc.StorageI16 {
			valueKind = gc.StorageI32
		}
		return gc.Value{Kind: valueKind, Bits: bits}
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
		actual, err := in.gc.ObjectType(ref)
		if err != nil || int(actual) >= len(in.c.Types) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array reference element is invalid: %v", err)})
		}
		exact := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Exact: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(actual)}}}
		if !valueTypeSubtype(exact, in.c.Types, want, in.c.Types) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array reference type %d does not match destination type %d", actual, typeID)})
		}
		return gc.RefValue(ref)
	}

	switch helper {
	case gcArrayInitElem:
		if len(args) != 6 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array init-elem helper arity = %d, want 6", len(args))})
		}
		ref, dstStart := gc.Ref(uint32(args[0])), uint32(args[1])
		srcStart, length := uint32(args[2]), uint32(args[3])
		typeID, elemIndex := uint32(args[4]), uint32(args[5])
		if in.c.stagedGCArrayProduct() != stagedGCArrayProductInitElem || typeID != 2 || elemIndex != 0 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem product/type/segment %s/%d/%d is unavailable", in.c.stagedGCArrayProduct(), typeID, elemIndex)})
		}
		checkArray(ref, typeID)
		if len(in.c.passiveElems) != 1 || len(in.c.Types) <= int(typeID) || in.c.Types[typeID].Kind != CompositeTypeArray {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem metadata is unavailable")})
		}
		dstLen, err := in.gc.ArrayLen(ref)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if uint64(dstStart)+uint64(length) > uint64(dstLen) {
			panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
		}
		descPtr := in.jm.PassiveElemPtr()
		if descPtr == 0 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem has no passive descriptor")})
		}
		desc := unsafe.Slice((*byte)(offHeapPtr(descPtr)), coreruntime.PassiveElemDescBytes)
		segmentLen := binary.LittleEndian.Uint32(desc[8:])
		if uint64(srcStart)+uint64(length) > uint64(segmentLen) {
			panic(gcStructHelperTrap{code: coreruntime.TrapIndirectOutOfBounds})
		}
		if length > 12 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem length %d exceeds exact bound 12", length)})
		}
		entryPtr := uintptr(binary.LittleEndian.Uint64(desc))
		if length != 0 && entryPtr == 0 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem segment entries are unavailable")})
		}
		want := in.c.Types[typeID].Array.Storage.Value
		var words [12]uint64
		for i := uint32(0); i < length; i++ {
			off := uint64(srcStart+i) * uint64(coreruntime.TableEntryBytes)
			if off+coreruntime.TableEntryBytes > uint64(segmentLen)*uint64(coreruntime.TableEntryBytes) {
				panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem source offset overflow")})
			}
			entry := unsafe.Slice((*byte)(offHeapPtr(entryPtr+uintptr(off))), coreruntime.TableEntryBytes)
			identity := binary.LittleEndian.Uint64(entry[coreruntime.TableEntryRefSlotOffset:])
			if identity == 0 {
				if !want.Ref.Nullable {
					panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem source %d is null for non-null destination", srcStart+i)})
				}
				words[i] = 0
				continue
			}
			base := uintptr(unsafe.Pointer(&in.funcRefDescs[0]))
			ptr := uintptr(identity)
			if ptr < base+coreruntime.TableEntryBytes || ptr >= base+uintptr(len(in.funcRefDescs)) || (ptr-base)%coreruntime.TableEntryBytes != 0 {
				panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem source %d has foreign function identity %#x", srcStart+i, identity)})
			}
			fidx := uint32((ptr-base)/coreruntime.TableEntryBytes - 1)
			actual, err := in.c.functionRefExactType(fidx)
			if err != nil || !valueTypeSubtype(actual, in.c.Types, want, in.c.Types) {
				panic(gcStructHelperError{err: fmt.Errorf("gc array.init_elem source %d type mismatch: %v", srcStart+i, err)})
			}
			words[i] = identity
		}
		// This exact product stores local funcref descriptor identities in an i64
		// payload. They are owned by the instance, not by the collector, so object,
		// card, and collector bulk barriers are deliberately inapplicable.
		if err := in.gc.ArrayInitWords(ref, dstStart, words[:length]); err != nil {
			if strings.Contains(err.Error(), "range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
			}
			panic(gcStructHelperError{err: err})
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
		if len(args) != 5 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array fill helper arity = %d, want 5", len(args))})
		}
		ref, start, typeID := gc.Ref(uint32(args[0])), uint32(args[1]), uint32(args[4])
		checkArray(ref, typeID)
		var value gc.Value
		if int(typeID) < len(in.c.GCTypeDescs) && (in.c.GCTypeDescs[typeID].Elem == gc.StorageRef || in.c.GCTypeDescs[typeID].Elem == gc.StorageRefNull) {
			value = arrayRefValue(typeID, args[2])
		} else {
			value = arrayValue(typeID, args[2])
		}
		if err := in.gc.ArrayFill(ref, start, value, uint32(args[3])); err != nil {
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
		if (in.c.stagedGCArrayProduct() == stagedGCArrayProductInitElem || in.c.stagedGCArrayProduct() == stagedGCArrayProductNewElem) && int(elemIndex) < len(in.c.passiveElems) && in.jm.PassiveElemPtr() != 0 {
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
		if in.c.stagedGCArrayProduct() == stagedGCArrayProductNewElem {
			if int(elemIndex) >= len(in.c.passiveElems) || in.jm.PassiveElemPtr() == 0 || int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray || in.c.GCTypeDescs[typeID].Elem != gc.StorageI64 {
				panic(gcStructHelperError{err: fmt.Errorf("gc array element segment/type %d/%d is unavailable", elemIndex, typeID)})
			}
			descOff := int(elemIndex) * coreruntime.PassiveElemDescBytes
			desc := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr()+uintptr(descOff))), coreruntime.PassiveElemDescBytes)
			segmentLen := binary.LittleEndian.Uint32(desc[8:])
			if uint64(source)+uint64(length) > uint64(segmentLen) {
				panic(gcStructHelperTrap{code: coreruntime.TrapIndirectOutOfBounds})
			}
			entryBytes := elemEntryBytes(in.c.passiveElems[elemIndex].RefType)
			entriesPtr := uintptr(binary.LittleEndian.Uint64(desc))
			if length != 0 && entriesPtr == 0 {
				panic(gcStructHelperError{err: fmt.Errorf("gc array element segment %d has no entries", elemIndex)})
			}
			ref, err := in.gc.NewArrayDefaultWithRoots(gc.TypeID(typeID), length, gc.EmptyRoots{})
			if err != nil {
				panic(gcStructHelperError{err: err})
			}
			for i := uint32(0); i < length; i++ {
				entry := unsafe.Slice((*byte)(offHeapPtr(entriesPtr+uintptr(source+i)*uintptr(entryBytes))), entryBytes)
				var bits uint64
				if entryBytes == coreruntime.TableEntryBytes {
					bits = binary.LittleEndian.Uint64(entry[coreruntime.TableEntryRefSlotOffset:])
				} else {
					bits = binary.LittleEndian.Uint64(entry)
				}
				if err := in.gc.ArraySet(ref, i, gc.Value{Kind: gc.StorageI64, Bits: bits}); err != nil {
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
		roots := &state.AllocRoots
		clear(roots.Values[:])
		roots.Count = uint8(length)
		defer func() {
			clear(roots.Values[:])
			roots.Count = 0
		}()
		for i := uint8(0); i < roots.Count; i++ {
			rooted, err := in.gc.CheckedTableSlot(state.Slots[uint8(source)+i])
			if err != nil || rooted.IsNull() {
				panic(gcStructHelperError{err: fmt.Errorf("gc array element root %d is unavailable: %v", uint32(source)+uint32(i), err)})
			}
			roots.Values[i] = gc.Root(rooted)
			_ = arrayRefValue(typeID, uint64(rooted))
		}
		var ref gc.Ref
		var err error
		if length == 0 {
			ref, err = in.gc.NewArrayDefaultWithRoots(gc.TypeID(typeID), 0, roots)
		} else {
			ref, err = in.gc.NewRefArrayWithRoots(gc.TypeID(typeID), length, &roots.Values[0], roots)
		}
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		for i := uint8(0); i < roots.Count; i++ {
			if err := in.gc.ArraySet(ref, uint32(i), arrayRefValue(typeID, uint64(roots.ref(i)))); err != nil {
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
		ref, err := in.gc.NewArrayDefaultWithRoots(gc.TypeID(typeID), length, gc.EmptyRoots{})
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		for i := uint32(0); i < length; i++ {
			off := uint64(source) + uint64(i)*width
			var bits uint64
			switch width {
			case 1:
				bits = uint64(data[off])
			case 2:
				bits = uint64(binary.LittleEndian.Uint16(data[off : off+2]))
			case 4:
				bits = uint64(binary.LittleEndian.Uint32(data[off : off+4]))
			case 8:
				bits = binary.LittleEndian.Uint64(data[off : off+8])
			}
			if err := in.gc.ArraySet(ref, i, gc.Value{Kind: storage, Bits: bits}); err != nil {
				panic(gcStructHelperError{err: err})
			}
		}
		results[0] = uint64(ref)
	case gcArrayAllocUniform:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-uniform helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		typeID, length := uint32(args[2]), uint32(args[1])
		value := arrayValue(typeID, args[0])
		ref, err := in.gc.NewArrayDefaultWithRoots(gc.TypeID(typeID), length, gc.EmptyRoots{})
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		for i := uint32(0); i < length; i++ {
			if err := in.gc.ArraySet(ref, i, value); err != nil {
				panic(gcStructHelperError{err: err})
			}
		}
		results[0] = uint64(ref)
	case gcArrayAllocFixed:
		if len(args) < 2 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-fixed helper arity = %d/%d, want at-least-2/at-least-1", len(args), len(results))})
		}
		count := uint32(args[len(args)-1])
		typeID := uint32(args[len(args)-2])
		if int(count)+2 != len(args) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-fixed count = %d, args = %d", count, len(args))})
		}
		ref, err := in.gc.NewArrayDefaultWithRoots(gc.TypeID(typeID), count, gc.EmptyRoots{})
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		for i := uint32(0); i < count; i++ {
			if err := in.gc.ArraySet(ref, i, arrayValue(typeID, args[i])); err != nil {
				panic(gcStructHelperError{err: err})
			}
		}
		results[0] = uint64(ref)
	case gcArrayAllocDefault:
		if len(args) != 2 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-default helper arity = %d/%d, want 2/at-least-1", len(args), len(results))})
		}
		ref, err := in.gc.NewArrayDefaultWithRoots(gc.TypeID(uint32(args[1])), uint32(args[0]), gc.EmptyRoots{})
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = uint64(ref)
	case gcArrayGet, gcArrayGetS, gcArrayGetU:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array get helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		ref, typeID := gc.Ref(uint32(args[0])), uint32(args[2])
		checkArray(ref, typeID)
		value, err := in.gc.ArrayGet(ref, uint32(args[1]))
		if err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
			}
			panic(gcStructHelperError{err: err})
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
			}
		}
	case gcArraySet:
		if len(args) != 4 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array set helper arity = %d, want 4", len(args))})
		}
		ref, typeID := gc.Ref(uint32(args[0])), uint32(args[3])
		checkArray(ref, typeID)
		var value gc.Value
		if int(typeID) < len(in.c.GCTypeDescs) && (in.c.GCTypeDescs[typeID].Elem == gc.StorageRef || in.c.GCTypeDescs[typeID].Elem == gc.StorageRefNull) {
			value = arrayRefValue(typeID, args[2])
		} else {
			value = arrayValue(typeID, args[2])
		}
		if err := in.gc.ArraySet(ref, uint32(args[1]), value); err != nil {
			if strings.Contains(err.Error(), "index out of range") {
				panic(gcStructHelperTrap{code: coreruntime.TrapBuiltin})
			}
			panic(gcStructHelperError{err: err})
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
