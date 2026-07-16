package wago

import (
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
)

type gcStructHelperError struct{ err error }
type gcStructHelperTrap struct{ code coreruntime.TrapCode }

func (e gcStructHelperError) Error() string { return e.err.Error() }

func (in *Instance) dispatchGCStructHelper(helper uint32, args, results []uint64) {
	if in == nil || in.gc == nil {
		panic(gcStructHelperError{err: fmt.Errorf("gc struct helper %d has no live collector", helper)})
	}
	state := in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	switch helper {
	case gcStructAllocDefault:
		if len(args) != 1 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct alloc helper arity = %d/%d, want 1/at-least-1", len(args), len(results))})
		}
		// Exact local products have no live frame ref across allocation. The
		// ref.test table product may retain prior objects only in checked collector
		// table slots, and stores each returned ref before the next allocation.
		// A non-nil empty frame-root set keeps stress collection explicit.
		ref, err := in.gc.NewStructDefaultWithRoots(gc.TypeID(uint32(args[0])), gc.EmptyRoots{})
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
		want := gc.TypeID(uint32(args[1]))
		if actual != want {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct get type = %d, want %d", actual, want)})
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
		}
	case gcStructRefTest:
		if len(args) != 3 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc ref.test helper arity = %d/%d, want 3/at-least-1", len(args), len(results))})
		}
		heap := int64(args[1])
		target := gc.RefTestTarget{Nullable: args[2] != 0}
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
				panic(gcStructHelperError{err: fmt.Errorf("gc ref.test heap type %d is unavailable", heap)})
			}
			target.Kind, target.Type = gc.RefTestDefined, gc.TypeID(heap)
		}
		var matched bool
		var err error
		if state := in.existingGCRefTestTableState(); state != nil && state.CanonicalType != nil {
			matched, err = in.gc.RefTestCanonical(gc.Ref(uint32(args[0])), target, state.CanonicalType)
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
	case gcStructTableSet:
		if len(args) != 3 || args[2] != 0 {
			panic(gcStructHelperError{err: fmt.Errorf("gc ref.test table-set helper args = %v, want index/ref/table-0", args)})
		}
		state := in.existingGCRefTestTableState()
		if state == nil {
			panic(gcStructHelperError{err: fmt.Errorf("gc ref.test table state is unavailable")})
		}
		index := args[0]
		if index >= uint64(state.Count) {
			panic(gcStructHelperTrap{code: coreruntime.TrapIndirectOutOfBounds})
		}
		if err := state.set(in.gc, index, gc.Ref(uint32(args[1]))); err != nil {
			panic(gcStructHelperError{err: err})
		}
	case gcStructSet:
		if len(args) != 4 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set helper arity = %d, want 4", len(args))})
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcStructHelperTrap{code: coreruntime.TrapNullReference})
		}
		typeID := uint32(args[2])
		fieldID := uint32(args[3])
		actual, err := in.gc.ObjectType(ref)
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		if actual != gc.TypeID(typeID) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set type = %d, want %d", actual, typeID)})
		}
		if int(typeID) >= len(in.c.GCTypeDescs) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct set field %d:%d is unavailable", typeID, fieldID)})
		}
		kind := in.c.GCTypeDescs[typeID].Fields[fieldID].Kind
		if kind == gc.StorageRef || kind == gc.StorageRefNull {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct reference-field set remains outside the staged helper slice")})
		}
		valueKind := kind
		if kind == gc.StorageI8 || kind == gc.StorageI16 {
			valueKind = gc.StorageI32
		}
		if err := in.gc.StructSet(ref, fieldID, gc.Value{Kind: valueKind, Bits: args[1]}); err != nil {
			panic(gcStructHelperError{err: err})
		}
	default:
		panic(gcStructHelperError{err: fmt.Errorf("unknown gc struct helper %d", helper)})
	}
}
