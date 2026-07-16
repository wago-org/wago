package wago

import (
	"encoding/binary"
	"fmt"
	"strings"

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

	switch helper {
	case gcArrayAllocData:
		if len(args) != 4 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array alloc-data helper arity = %d/%d, want 4/at-least-1", len(args), len(results))})
		}
		source, length := uint32(args[0]), uint32(args[1])
		typeID, dataIndex := uint32(args[2]), uint32(args[3])
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindArray || in.c.GCTypeDescs[typeID].Elem != gc.StorageI8 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data type %d is not an admitted i8 array", typeID)})
		}
		if int(dataIndex) >= len(in.c.PassiveData) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data segment %d is unavailable", dataIndex)})
		}
		descOff := int(dataIndex) * coreruntime.PassiveDataDescBytes
		if descOff < 0 || descOff+coreruntime.PassiveDataDescBytes > len(in.passiveDataDesc) {
			panic(gcStructHelperError{err: fmt.Errorf("gc array.new_data segment %d has no instance descriptor", dataIndex)})
		}
		segmentLen := binary.LittleEndian.Uint32(in.passiveDataDesc[descOff+8:])
		end := uint64(source) + uint64(length)
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
			if err := in.gc.ArraySet(ref, i, gc.I32Value(int32(data[uint64(source)+uint64(i)]))); err != nil {
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
			results[0] = value.Bits
		}
	case gcArraySet:
		if len(args) != 4 {
			panic(gcStructHelperError{err: fmt.Errorf("gc array set helper arity = %d, want 4", len(args))})
		}
		ref, typeID := gc.Ref(uint32(args[0])), uint32(args[3])
		checkArray(ref, typeID)
		if err := in.gc.ArraySet(ref, uint32(args[1]), arrayValue(typeID, args[2])); err != nil {
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
