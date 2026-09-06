package wago

import (
	"encoding/binary"
	"errors"
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Internal GC helper dispatch occupies bit 30. Public host-funcref dispatch uses
// bit 31, and ordinary Wasm import indexes use neither. The amd64 backend mirrors
// these compile-only constants.
const (
	gcStructDispatchBit       uint32 = 1 << 30
	gcStructAllocDefault             = 1
	gcStructGet                      = 2
	gcStructSet                      = 3
	gcStructGetS                     = 4
	gcStructGetU                     = 5
	gcStructRefTest                  = 6
	gcStructTableSet                 = 7
	gcAnyConvertExtern               = 8
	gcExternConvertAny               = 9
	gcStructRefCast                  = 10
	gcStructAllocOne                 = 11
	gcStructFinalCastGet             = 12
	gcStructFinalCastArrayLen        = 13
	gcFuncRefTest                    = 14
	gcStructReserveDead              = 15
)

type gcStructHelperError struct{ err error }
type gcStructHelperTrap struct{ code coreruntime.TrapCode }

func (e gcStructHelperError) Error() string { return e.err.Error() }

func (in *Instance) gcObjectTypeMatches(actual gc.TypeID, want uint32) bool {
	if in == nil || in.gc == nil {
		return false
	}
	required, ok := in.gcDomainType(want)
	if !ok {
		return false
	}
	if int(want) < len(in.c.Types) && in.c.Types[want].Final {
		return actual == required
	}
	matched, err := in.gc.TypeSubtype(actual, required)
	return err == nil && matched
}

func gcHelperMayAllocate(helper uint32) bool {
	switch helper {
	case gcStructAllocDefault, gcStructAllocOne, gcStructReserveDead,
		gcArrayAllocDefault, gcArrayAllocFixed, gcArrayAllocUniform,
		gcArrayAllocData, gcArrayAllocElem, gcArrayAllocFixedV128Spill,
		gcArrayAllocDefaultNative, gcArrayAllocUniformNative, gcArrayAllocFixedNative,
		gcArrayCheckDefault, gcArrayCheckUniform, gcArrayCheckData, gcArrayCheckFixed:
		return true
	default:
		return false
	}
}

//lint:ignore U1000 used by builds with wago_gcstats enabled
func gcHelperMayMutate(helper uint32) bool {
	switch helper {
	case gcStructSet, gcStructTableSet,
		gcArraySet, gcArrayDropElem, gcArrayFill, gcArrayCopy,
		gcArrayInitData, gcArrayInitElem:
		return true
	default:
		return false
	}
}

func (in *Instance) dispatchGCStructHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64) {
	if in == nil || (in.gc == nil && helper != gcFuncRefTest) {
		panic(gcStructHelperError{err: fmt.Errorf("gc struct helper %d has no live collector", helper)})
	}
	var lockedDomain *gcStoreDomain
	if in.gc != nil && helper != gcFuncRefTest {
		lockedDomain = in.lockGCCollector()
	}
	defer unlockGCCollector(lockedDomain)
	recordSynchronousGCHelper(in, helper, args)
	var state *gcPublicState
	var frameRoots gc.RootSet = gc.EmptyRoots{}
	if gcHelperMayAllocate(helper) {
		// Native batches own unpublished handles and a top nursery chunk. Cancel
		// them before any Go allocation, trap, or collecting fallback so the helper
		// sees the exact used bump and cannot overlap reserved bytes.
		in.gc.CancelNativeAllocationBatch()
		state = in.publicGCState()
		state.mu.Lock()
		defer state.mu.Unlock()
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
	structValueKnown := func(typeID, fieldID uint32, kind gc.StorageKind, want ValueTypeDescriptor, words []uint64) gc.Value {
		wantSlots := 1
		if kind == gc.StorageV128 {
			wantSlots = 2
		}
		if len(words) != wantSlots {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct field %d:%d value uses %d slot(s), want %d", typeID, fieldID, len(words), wantSlots)})
		}
		bits := words[0]
		if kind == gc.StorageFuncRef || kind == gc.StorageFuncRefNull {
			if bits == 0 {
				if kind == gc.StorageFuncRef {
					panic(gcStructHelperError{err: fmt.Errorf("gc struct field %d:%d rejects null funcref", typeID, fieldID)})
				}
				return gc.Value{Kind: kind}
			}
			actual, actualTypes, ok := in.attachedFuncrefExactType(bits)
			if !ok && in.refStore != nil {
				actual, actualTypes, ok = in.refStore.descriptorFuncrefExactType(in, bits)
			}
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
		if !in.gcRefMatchesValueType(ref, want) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct reference type does not match field %d:%d", typeID, fieldID)})
		}
		return gc.RefValue(ref)
	}
	structValue := func(typeID, fieldID uint32, words []uint64) gc.Value {
		kind := structFieldKind(typeID, fieldID)
		return structValueKnown(typeID, fieldID, kind, in.c.Types[typeID].Fields[fieldID].Storage.Value, words)
	}
	switch helper {
	case gcFuncRefTest:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("function ref.test helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		bits, targetType, nullable := args[0], uint32(args[1]), args[2] != 0
		if int(targetType) >= len(in.c.Types) || in.c.Types[targetType].Kind != CompositeTypeFunction {
			panic(gcStructHelperError{err: fmt.Errorf("function ref.test target type %d is unavailable", targetType)})
		}
		if bits == 0 {
			if nullable {
				results[0] = 1
			} else {
				results[0] = 0
			}
			break
		}
		actual, actualTypes, ok := in.attachedFuncrefExactType(bits)
		if !ok && in.refStore != nil {
			actual, actualTypes, ok = in.refStore.descriptorFuncrefExactType(in, bits)
		}
		want := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: nullable, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: targetType}}}
		if ok && valueTypeSubtype(actual, actualTypes, want, in.c.Types) {
			results[0] = 1
		} else {
			results[0] = 0
		}
	case gcStructReserveDead:
		if len(args) != 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct dead reservation helper arity = %d, want 1", len(args))})
		}
		ref, err := in.gc.ReserveDeadStructAllocation(in.requireGCDomainType(uint32(args[0])), frameRoots)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
	case gcStructAllocDefault:
		if len(args) != 1 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct alloc helper arity = %d/%d, want 1/at-least-1", len(args), len(results))})
		}
		// Exact local products have no live frame ref across allocation. The
		// ref.test table product may retain prior objects only in checked collector
		// table slots, and stores each returned ref before the next allocation.
		// A non-nil empty frame-root set keeps stress collection explicit.
		ref, err := in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(uint32(args[0])), frameRoots)
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
		descFields := in.c.GCTypeDescs[typeID].Fields
		typeFields := in.c.Types[typeID].Fields
		cursor := 0
		for i := range descFields {
			kind := descFields[i].Kind
			slots := 1
			if kind == gc.StorageV128 {
				slots = 2
			}
			if cursor+slots > len(args)-1 {
				panic(gcStructHelperError{err: fmt.Errorf("gc struct type %d initializer slots end at %d, args = %d", typeID, cursor+slots, len(args))})
			}
			words := args[cursor : cursor+slots]
			switch kind {
			case gc.StorageRef, gc.StorageRefNull,
				gc.StorageFuncRef, gc.StorageFuncRefNull,
				gc.StorageExternRef, gc.StorageExternRefNull:
				_ = structValueKnown(typeID, uint32(i), kind, typeFields[i].Storage.Value, words)
			}
			cursor += slots
		}
		if cursor != len(args)-1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct type %d initializer uses %d slots, args = %d", typeID, cursor, len(args))})
		}
		ref, err := in.gc.NewStructWordsPrevalidatedWithRootScratch(in.requireGCDomainType(typeID), args[:len(args)-1], frameRoots, &state.initializerRoots)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = uint64(ref)
		in.prepareNativeStructHandles(typeID)
	case gcStructGet, gcStructGetS, gcStructGetU:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct get helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		want := uint32(args[1])
		required, ok := in.gcDomainType(want)
		if !ok {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct get type %d has no Runtime-domain identity", want)})
		}
		exact := int(want) < len(in.c.Types) && in.c.Types[want].Final
		value, actual, matched, err := in.gc.StructGetTyped(ref, required, exact, uint32(args[2]))
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if !matched {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct get type = %d, want subtype of %d", actual, want)})
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
		if len(args) != 4 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc ref.test helper arity = %d/%d, want 4/at-least-1", len(args), len(results))})
		}
		target, err := in.gcDynamicRefTarget(int64(args[1]), args[2] != 0, args[3] != 0)
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
		if len(args) != 4 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc ref.cast helper arity = %d/%d, want 4/at-least-1", len(args), len(results))})
		}
		exact := args[3] != 0
		if value, handled, err := in.gcDefinedRefCast(args[0], int64(args[1]), args[2] != 0, exact); handled {
			if errors.Is(err, gc.ErrCastFailure) {
				panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
			}
			if err != nil {
				panic(gcStructHelperError{err: err})
			}
			results[0] = value
			break
		}
		target, err := in.gcDynamicRefTarget(int64(args[1]), args[2] != 0, exact)
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
	case gcStructFinalCastGet:
		if len(args) != 5 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/get helper arity = %d/%d, want 5/at-least-1", len(args), len(results))})
		}
		bits, typeID, fieldID, nullable, mode := args[0], uint32(args[1]), uint32(args[2]), args[3] != 0, uint32(args[4])
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/get contains non-compact reference %#x", bits)})
		}
		if ref.IsNull() {
			if nullable {
				panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
			}
			panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
		}
		if ref.IsI31() {
			panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
		}
		if int(typeID) >= len(in.c.Types) || int(typeID) >= len(in.c.GCTypeDescs) || !in.c.Types[typeID].Final || in.c.Types[typeID].Kind != CompositeTypeStruct || int(fieldID) >= len(in.c.Types[typeID].Fields) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/get field %d:%d is unavailable", typeID, fieldID)})
		}
		kind := in.c.GCTypeDescs[typeID].Fields[fieldID].Kind
		if mode > 2 || (mode == 0 && (kind == gc.StorageI8 || kind == gc.StorageI16)) || (mode != 0 && kind != gc.StorageI8 && kind != gc.StorageI16) {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/get field %d:%d kind %d rejects mode %d", typeID, fieldID, kind, mode)})
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/get type %d has no Runtime-domain identity", typeID)})
		}
		if mode == 0 && (kind == gc.StorageRef || kind == gc.StorageRefNull) {
			value, matched, err := in.gc.StructGetFinalRef(ref, required, fieldID)
			if err != nil {
				panic(gcStructHelperError{err: err})
			}
			if !matched {
				panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
			}
			results[0] = uint64(value)
			break
		}
		value, _, matched, err := in.gc.StructGetTyped(ref, required, true, fieldID)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if !matched {
			panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
		}
		switch mode {
		case 1:
			if value.Kind == gc.StorageI8 {
				results[0] = uint64(uint32(int32(int8(value.Bits))))
			} else {
				results[0] = uint64(uint32(int32(int16(value.Bits))))
			}
		case 2:
			if value.Kind == gc.StorageI8 {
				results[0] = uint64(uint32(uint8(value.Bits)))
			} else {
				results[0] = uint64(uint32(uint16(value.Bits)))
			}
		default:
			if value.Kind == gc.StorageRef || value.Kind == gc.StorageRefNull {
				results[0] = uint64(value.Ref)
			} else {
				results[0] = value.Bits
				if value.Kind == gc.StorageV128 {
					if len(results) < 2 {
						panic(gcStructHelperError{err: fmt.Errorf("gc final cast/get v128 result arity = %d, want at least 2", len(results))})
					}
					results[1] = value.BitsHi
				}
			}
		}
	case gcStructFinalCastArrayLen:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/array.len helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		bits, typeID, nullable := args[0], uint32(args[1]), args[2] != 0
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/array.len contains non-compact reference %#x", bits)})
		}
		if ref.IsNull() {
			if nullable {
				panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
			}
			panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
		}
		if ref.IsI31() {
			panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
		}
		if int(typeID) >= len(in.c.Types) || int(typeID) >= len(in.c.GCTypeDescs) || !in.c.Types[typeID].Final || in.c.Types[typeID].Kind != CompositeTypeArray || in.c.GCTypeDescs[typeID].Kind != gc.KindArray {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/array.len type %d is unavailable", typeID)})
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcStructHelperError{err: fmt.Errorf("gc final cast/array.len type %d has no Runtime-domain identity", typeID)})
		}
		length, _, matched, err := in.gc.ArrayLenTyped(ref, required, true)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if !matched {
			panic(gcStructHelperTrap{code: coreruntime.TrapCastFailure})
		}
		results[0] = uint64(length)
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
		if int(typeID) >= len(in.c.GCTypeDescs) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set field %d:%d is unavailable", typeID, fieldID)})
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set type %d has no Runtime-domain identity", typeID)})
		}
		exact := int(typeID) < len(in.c.Types) && in.c.Types[typeID].Final
		actual, matched, err := in.gc.StructSetTyped(ref, required, exact, fieldID, structValue(typeID, fieldID, args[1:1+valueSlots]))
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if !matched {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set type = %d, want subtype of %d", actual, typeID)})
		}
	default:
		panic(gcStructHelperError{err: fmt.Errorf("unknown gc struct helper %d", helper)})
	}
}

// gcDefinedRefCast avoids descriptor/subtype walks when equality is the complete
// decision: either the encoded target is exact or the declared target is final.
func (in *Instance) gcDefinedRefCast(bits uint64, heap int64, nullable, exact bool) (uint64, bool, error) {
	if in == nil || in.gc == nil || in.existingGCRefTestTableState() != nil || heap < 0 || uint64(heap) >= uint64(len(in.c.Types)) {
		return 0, false, nil
	}
	type_ := in.c.Types[heap]
	if (!exact && !type_.Final) || (type_.Kind != CompositeTypeStruct && type_.Kind != CompositeTypeArray) {
		return 0, false, nil
	}
	ref := gc.Ref(uint32(bits))
	if bits != uint64(ref) {
		return 0, true, fmt.Errorf("gc ref.cast contains non-compact reference %#x", bits)
	}
	if ref.IsNull() {
		if nullable {
			return 0, true, nil
		}
		return 0, true, gc.ErrCastFailure
	}
	if ref.IsI31() {
		return 0, true, gc.ErrCastFailure
	}
	actual, err := in.gc.ObjectType(ref)
	if err != nil {
		return 0, true, err
	}
	required, ok := in.gcDomainType(uint32(heap))
	if !ok {
		return 0, true, fmt.Errorf("gc dynamic reference heap type %d has no Runtime-domain identity", heap)
	}
	if actual != required {
		return 0, true, gc.ErrCastFailure
	}
	return bits, true, nil
}

func (in *Instance) gcDynamicRefTarget(heap int64, nullable, exact bool) (gc.RefTestTarget, error) {
	target := gc.RefTestTarget{Nullable: nullable, Exact: exact}
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
		domain, ok := in.gcDomainType(uint32(heap))
		if !ok {
			return gc.RefTestTarget{}, fmt.Errorf("gc dynamic reference heap type %d has no Runtime-domain identity", heap)
		}
		target.Kind, target.Type = gc.RefTestDefined, domain
	}
	if exact && target.Kind != gc.RefTestDefined {
		return gc.RefTestTarget{}, fmt.Errorf("gc dynamic exact reference heap type %d is not defined", heap)
	}
	return target, nil
}
