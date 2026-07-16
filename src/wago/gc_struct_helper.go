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
)

type gcStructHelperError struct{ err error }
type gcStructHelperTrap struct{ code coreruntime.TrapCode }

func (e gcStructHelperError) Error() string { return e.err.Error() }

func (in *Instance) dispatchGCStructHelper(helper uint32, args, results []uint64) {
	if in == nil || in.gc == nil {
		panic(gcStructHelperError{err: fmt.Errorf("gc struct helper %d has no live collector", helper)})
	}
	switch helper {
	case gcStructAllocDefault:
		if len(args) != 1 || len(results) < 1 {
			panic(gcStructHelperError{err: fmt.Errorf("gc struct alloc helper arity = %d/%d, want 1/at-least-1", len(args), len(results))})
		}
		// The exact admitted product performs at most one allocation while no
		// prior gc.Ref is live. A non-nil empty root set is nevertheless supplied
		// so Throughput/Tiny stress collection remains explicit and fail-closed.
		ref, err := in.gc.NewStructDefaultWithRoots(gc.TypeID(uint32(args[0])), gc.EmptyRoots{})
		if err != nil {
			panic(gcStructHelperError{err: err})
		}
		results[0] = uint64(ref)
	case gcStructGet:
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
		} else {
			results[0] = value.Bits
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
		if err := in.gc.StructSet(ref, fieldID, gc.Value{Kind: kind, Bits: args[1]}); err != nil {
			panic(gcStructHelperError{err: err})
		}
	default:
		panic(gcStructHelperError{err: fmt.Errorf("unknown gc struct helper %d", helper)})
	}
}
