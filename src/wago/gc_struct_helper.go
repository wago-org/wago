package wago

import (
	"encoding/binary"
	"errors"
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// Internal GC helper dispatch occupies bit 30. Public host-funcref dispatch uses
// bit 31, and ordinary Wasm import indexes use neither. The amd64 backend mirrors
// these compile-only constants.
const (
	gcStructDispatchBit  uint32 = 1 << 30
	gcStructAllocDefault        = 1
	gcStructGet                 = 2
	gcStructSet                 = 3
	gcStructGetS                = 4
	gcStructGetU                = 5
	gcStructRefTest             = 6
	gcStructTableSet            = 7
	gcAnyConvertExtern          = 8
	gcExternConvertAny          = 9
	gcStructRefCast             = 10
	gcStructAllocOne            = 11
)

type gcStructHelperError struct{ err error }
type gcStructHelperTrap struct{ code coreruntime.TrapCode }

func (e gcStructHelperError) Error() string { return e.err.Error() }

func (in *Instance) gcObjectTypeMatches(actual gc.TypeID, want uint32) bool {
	if uint32(actual) == want {
		return true
	}
	if in == nil || in.c == nil || int(actual) >= len(in.c.Types) || int(want) >= len(in.c.Types) {
		return false
	}
	// Collector type IDs and helper immediates name the same validated module
	// graph, so runtime compatibility is declared-index reachability. Avoid the
	// general cross-module structural-equivalence machinery (and its maps) on this
	// hot path.
	var visit func(uint32, int) bool
	visit = func(index uint32, depth int) bool {
		if index == want {
			return true
		}
		if int(index) >= len(in.c.Types) || depth >= len(in.c.Types) {
			return false
		}
		for _, super := range in.c.Types[index].Supers {
			if visit(super, depth+1) {
				return true
			}
		}
		return false
	}
	if visit(uint32(actual), 0) {
		return true
	}
	actualType := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{
		Exact: true,
		Heap:  HeapTypeDescriptor{Defined: true, TypeIndex: uint32(actual)},
	}}
	wantType := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{
		Heap: HeapTypeDescriptor{Defined: true, TypeIndex: want},
	}}
	return valueTypeSubtype(actualType, in.c.Types, wantType, in.c.Types)
}

func (in *Instance) dispatchGCStructHelper(helper uint32, args, results []uint64) {
	in.dispatchGCStructHelperParked(0, helper, 0, args, results)
}

func gcHelperMayAllocate(helper uint32) bool {
	switch helper {
	case gcStructAllocDefault, gcStructAllocOne,
		gcArrayAllocDefault, gcArrayAllocFixed, gcArrayAllocUniform,
		gcArrayAllocData, gcArrayAllocElem, gcArrayAllocFixedV128Spill:
		return true
	default:
		return false
	}
}

func (in *Instance) dispatchGCStructHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64) {
	if in == nil || in.gc == nil {
		panic(gcStructHelperError{err: fmt.Errorf("gc struct helper %d has no live collector", helper)})
	}
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state := in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	var frameRoots gc.RootSet = gc.EmptyRoots{}
	if gcHelperMayAllocate(helper) {
		if err := in.syncGenericGCGlobalRootsLocked(state); err != nil {
			panic(gcStructHelperError{err: err})
		}
		frameRoots = in.gcHelperRoots(ctrl, state, safepoint)
	}
	structFieldKind := func(typeID, fieldID uint32) gc.StorageKind {
		if int(typeID) >= len(in.c.GCTypeDescs) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) || int(typeID) >= len(in.c.Types) || in.c.Types[typeID].Kind != CompositeTypeStruct || int(fieldID) >= len(in.c.Types[typeID].Fields) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct field %d:%d is unavailable", typeID, fieldID)})
		}
		return in.c.GCTypeDescs[typeID].Fields[fieldID].Kind
	}
	structValueSlots := func(typeID, fieldID uint32) int {
		if structFieldKind(typeID, fieldID) == gc.StorageV128 {
			return 2
		}
		return 1
	}
	structValue := func(typeID, fieldID uint32, words []uint64) gc.Value {
		kind := structFieldKind(typeID, fieldID)
		wantSlots := 1
		if kind == gc.StorageV128 {
			wantSlots = 2
		}
		if len(words) != wantSlots {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct field %d:%d value uses %d slot(s), want %d", typeID, fieldID, len(words), wantSlots)})
		}
		bits := words[0]
		want := in.c.Types[typeID].Fields[fieldID].Storage.Value
		if kind == gc.StorageFuncRef || kind == gc.StorageFuncRefNull {
			if bits == 0 {
				if kind == gc.StorageFuncRef {
					panic(gcStructHelperError{err: fmt.Errorf("gc struct field %d:%d rejects null funcref", typeID, fieldID)})
				}
				return gc.Value{Kind: kind}
			}
			actual, actualTypes, ok := instanceFuncrefExactType(in, bits)
			if !ok || !valueTypeSubtype(actual, actualTypes, want, in.c.Types) {
				panic(gcStructHelperError{err: fmt.Errorf("gc struct funcref type does not match field %d:%d", typeID, fieldID)})
			}
			return gc.Value{Kind: kind, Bits: bits}
		}
		if kind == gc.StorageExternRef || kind == gc.StorageExternRefNull {
			if bits == 0 && kind == gc.StorageExternRef {
				panic(gcStructHelperError{err: fmt.Errorf("gc struct field %d:%d rejects null externref", typeID, fieldID)})
			}
			return gc.Value{Kind: kind, Bits: bits}
		}
		if kind != gc.StorageRef && kind != gc.StorageRefNull {
			valueKind := kind
			if kind == gc.StorageI8 || kind == gc.StorageI16 {
				valueKind = gc.StorageI32
			}
			value := gc.Value{Kind: valueKind, Bits: bits}
			if kind == gc.StorageV128 {
				value.BitsHi = words[1]
			}
			return value
		}
		ref := gc.Ref(uint32(bits))
		if ref.IsNull() {
			if !want.Ref.Nullable {
				panic(gcStructHelperError{err: fmt.Errorf("gc struct field %d:%d rejects null", typeID, fieldID)})
			}
			return gc.RefValue(ref)
		}
		var actual ValueTypeDescriptor
		if ref.IsI31() {
			actual = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Exact: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapI31}}}
		} else {
			actualType, err := in.gc.ObjectType(ref)
			if err != nil || int(actualType) >= len(in.c.Types) {
				panic(gcStructHelperError{err: fmt.Errorf("gc struct reference field %d:%d bits %#x is invalid for %+v: %v", typeID, fieldID, bits, want.Ref, err)})
			}
			actual = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Exact: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(actualType)}}}
		}
		if !valueTypeSubtype(actual, in.c.Types, want, in.c.Types) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct reference type does not match field %d:%d", typeID, fieldID)})
		}
		return gc.RefValue(ref)
	}
	switch helper {
	case gcStructAllocDefault:
		if len(args) != 1 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct alloc helper arity = %d/%d, want 1/at-least-1", len(args), len(results))})
		}
		// Exact local products have no live frame ref across allocation. The
		// ref.test table product may retain prior objects only in checked collector
		// table slots, and stores each returned ref before the next allocation.
		// A non-nil empty frame-root set keeps stress collection explicit.
		ref, err := in.gc.NewStructDefaultWithRoots(gc.TypeID(uint32(args[0])), frameRoots)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = uint64(ref)
	case gcStructAllocOne:
		if len(args) < 1 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct alloc helper arity = %d/%d, want at-least-1/at-least-1", len(args), len(results))})
		}
		typeID := uint32(args[len(args)-1])
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindStruct {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct type %d is unavailable", typeID)})
		}
		fieldCount := len(in.c.GCTypeDescs[typeID].Fields)
		if fieldCount > len(state.values) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct type %d exceeds %d helper initializer values", typeID, len(state.values))})
		}
		values := state.values[:fieldCount]
		cursor := 0
		for i := range values {
			slots := structValueSlots(typeID, uint32(i))
			if cursor+slots > len(args)-1 {
				panic(gcStructHelperError{err: fmt.Errorf("gc struct type %d initializer slots end at %d, args = %d", typeID, cursor+slots, len(args))})
			}
			values[i] = structValue(typeID, uint32(i), args[cursor:cursor+slots])
			cursor += slots
		}
		if cursor != len(args)-1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct type %d initializer uses %d slots, args = %d", typeID, cursor, len(args))})
		}
		ref, err := in.gc.NewStructWithRoots(gc.TypeID(typeID), values, frameRoots)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = uint64(ref)
	case gcStructGet, gcStructGetS, gcStructGetU:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct get helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		actual, err := in.gc.ObjectType(ref)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		want := uint32(args[1])
		if !in.gcObjectTypeMatches(actual, want) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct get type = %d, want subtype of %d", actual, want)})
		}
		value, err := in.gc.StructGet(ref, uint32(args[2]))
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if value.Kind == gc.StorageRef || value.Kind == gc.StorageRefNull {
			results[0] = uint64(value.Ref)
			break
		}
		switch helper {
		case gcStructGetS:
			switch value.Kind {
			case gc.StorageI8:
				results[0] = uint64(uint32(int32(int8(value.Bits))))
			case gc.StorageI16:
				results[0] = uint64(uint32(int32(int16(value.Bits))))
			default:
				panic(gcStructHelperError{err: fmt.Errorf("gc struct.get_s field kind %d is not packed", value.Kind)})
			}
		case gcStructGetU:
			switch value.Kind {
			case gc.StorageI8:
				results[0] = uint64(uint32(uint8(value.Bits)))
			case gc.StorageI16:
				results[0] = uint64(uint32(uint16(value.Bits)))
			default:
				panic(gcStructHelperError{err: fmt.Errorf("gc struct.get_u field kind %d is not packed", value.Kind)})
			}
		default:
			results[0] = value.Bits
			if value.Kind == gc.StorageV128 {
				if len(results) < 2 {
					panic(gcStructHelperError{err: fmt.Errorf("gc struct.get v128 result arity = %d, want at least 2", len(results))})
				}
				results[1] = value.BitsHi
			}
		}
	case gcStructRefTest:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc ref.test helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		target, err := gcDynamicRefTarget(int64(args[1]), args[2] != 0)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		var matched bool
		if state := in.existingGCRefTestTableState(); state != nil {
			matched, err = state.refTest(in.gc, args[0], target)
		} else {
			matched, err = in.gc.RefTest(gc.Ref(uint32(args[0])), target)
		}
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if matched {
			results[0] = 1
		} else {
			results[0] = 0
		}
	case gcStructRefCast:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc ref.cast helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		target, err := gcDynamicRefTarget(int64(args[1]), args[2] != 0)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		var value uint64
		if state := in.existingGCRefTestTableState(); state != nil {
			value, err = state.refCast(in.gc, args[0], target)
		} else {
			var ref gc.Ref
			ref, err = in.gc.RefCast(gc.Ref(uint32(args[0])), target)
			value = uint64(ref)
		}
		if errors.Is(err, gc.ErrCastFailure) {
			panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
		}
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = value
	case gcStructTableSet:
		if len(args) != 3 {
			panic(gcStructHelperError{err: fmt.Errorf("gc ref.test table-set helper args = %v, want index/ref/table", args)})
		}
		state := in.existingGCRefTestTableState()
		table, index := args[2], args[0]
		if state == nil {
			if table >= uint64(in.c.tableCount()) || !isGCRefValType(in.c.tableElementType(int(table))) {
				panic(gcStructHelperError{err: fmt.Errorf("gc ref.test table state is unavailable")})
			}
			descriptor := in.tableDescriptor(int(table))
			if len(descriptor) < 8 {
				panic(gcStructHelperError{err: fmt.Errorf("generic GC table %d descriptor is unavailable", table)})
			}
			if index >= uint64(binary.LittleEndian.Uint32(descriptor)) {
				panic(gcStructHelperTrap{code: coreruntime.TrapIndirectOutOfBounds})
			}
			word := args[1]
			ref := gc.Ref(uint32(word))
			if word != uint64(ref) {
				panic(gcStructHelperError{err: fmt.Errorf("generic GC table set contains non-compact reference %#x", word)})
			}
			binary.LittleEndian.PutUint64(descriptor[8+index*8:], word)
			in.gc.WriteBarrierRoot(ref)
			break
		}
		if table >= uint64(state.TableCount) || index >= uint64(binary.LittleEndian.Uint32(state.Descriptors[table])) {
			panic(gcStructHelperTrap{code: coreruntime.TrapIndirectOutOfBounds})
		}
		if err := state.setTable(in.gc, table, index, args[1]); err != nil {
			panic(gcStructHelperError{err: err})
		}
	case gcAnyConvertExtern, gcExternConvertAny:
		if len(args) != 1 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc extern conversion helper arity = %d/%d, want 1/at-least-1", len(args), len(results))})
		}
		state := in.existingGCRefTestTableState()
		if state == nil || state.Conversion == nil {
			panic(gcStructHelperError{err: fmt.Errorf("gc extern conversion state is unavailable")})
		}
		var value uint64
		var err error
		if helper == gcAnyConvertExtern {
			value, err = state.Conversion.anyFromExtern(args[0])
		} else {
			value, err = state.Conversion.externFromAny(args[0])
		}
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = value
	case gcStructSet:
		if len(args) < 4 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set helper arity = %d, want at least 4", len(args))})
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		typeID := uint32(args[len(args)-2])
		fieldID := uint32(args[len(args)-1])
		valueSlots := structValueSlots(typeID, fieldID)
		if len(args) != valueSlots+3 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set helper arity = %d, want %d", len(args), valueSlots+3)})
		}
		actual, err := in.gc.ObjectType(ref)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if !in.gcObjectTypeMatches(actual, typeID) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set type = %d, want subtype of %d", actual, typeID)})
		}
		if int(typeID) >= len(in.c.GCTypeDescs) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set field %d:%d is unavailable", typeID, fieldID)})
		}
		if err := in.gc.StructSet(ref, fieldID, structValue(typeID, fieldID, args[1:1+valueSlots])); err != nil {
			panic(gcStructHelperError{err: err})
		}
	default:
		panic(gcStructHelperError{err: fmt.Errorf("unknown gc struct helper %d", helper)})
	}
}

func gcDynamicRefTarget(heap int64, nullable bool) (gc.RefTestTarget, error) {
	target := gc.RefTestTarget{Nullable: nullable}
	switch heap {
	case -15:
		target.Kind = gc.RefTestNone
	case -18:
		target.Kind = gc.RefTestAny
	case -19:
		target.Kind = gc.RefTestEq
	case -20:
		target.Kind = gc.RefTestI31
	case -21:
		target.Kind = gc.RefTestStruct
	case -22:
		target.Kind = gc.RefTestArray
	default:
		if heap < 0 || uint64(heap) > uint64(^uint32(0)) {
			return gc.RefTestTarget{}, fmt.Errorf("gc dynamic reference heap type %d is unavailable", heap)
		}
		target.Kind, target.Type = gc.RefTestDefined, gc.TypeID(heap)
	}
	return target, nil
}
