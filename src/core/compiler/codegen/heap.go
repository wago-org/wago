package codegen

import (
	"errors"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// ErrUnsupportedHeapOp reports a heap operation unsupported by the selected
// allocator/GC policy.
var ErrUnsupportedHeapOp = errors.New("codegen: unsupported heap operation")

// UnsupportedHeapOpError adds operation and policy context to ErrUnsupportedHeapOp.
type UnsupportedHeapOpError struct {
	Policy string
	Op     string
}

func (e *UnsupportedHeapOpError) Error() string {
	if e.Policy == "" {
		return fmt.Sprintf("%v %s", ErrUnsupportedHeapOp, e.Op)
	}
	return fmt.Sprintf("%v %s for %s", ErrUnsupportedHeapOp, e.Op, e.Policy)
}

func (e *UnsupportedHeapOpError) Unwrap() error { return ErrUnsupportedHeapOp }

// HeapABI is the target-neutral allocator/GC codegen contract.
type HeapABI interface {
	Name() string
	RefLayout() RefLayout
	BeginModule(ModuleInfo) (ModuleHeapABI, error)
}

// ModuleHeapABI is heap lowering state scoped to one module.
type ModuleHeapABI interface {
	BeginFunc(FuncInfo) (FuncHeapABI, error)
}

// FuncHeapABI is heap lowering state scoped to one function.
type FuncHeapABI interface {
	AllocObject(Emitter, AllocObjectRequest) (Value, error)
	AllocArray(Emitter, AllocArrayRequest) (Value, error)

	LoadField(Emitter, FieldLoadRequest) (Value, error)
	StoreField(Emitter, FieldStoreRequest) error
	LoadArrayElem(Emitter, ArrayLoadRequest) (Value, error)
	StoreArrayElem(Emitter, ArrayStoreRequest) error
	ArrayLen(Emitter, ArrayLenRequest) (Value, error)

	WriteBarrier(Emitter, WriteBarrierRequest) error
	BulkWriteBarrier(Emitter, BulkWriteBarrierRequest) error
	Safepoint(Emitter, SafepointRequest) error

	EndFunc(Emitter) error
}

type AllocObjectRequest struct {
	TypeID     uint32
	Fields     []Value
	ResultType wasm.ValType
	LiveRefs   []Value // additional caller-known refs live across this may-allocate helper
}

type AllocArrayRequest struct {
	TypeID     uint32
	Length     Value
	Init       Value
	ResultType wasm.ValType
	LiveRefs   []Value // additional caller-known refs live across this may-allocate helper
}

type FieldLoadRequest struct {
	Object     Value
	TypeID     uint32
	Field      uint32
	Kind       gc.StorageKind
	ResultType wasm.ValType
	LiveRefs   []Value // additional caller-known refs live across this helper safepoint
}

type FieldStoreRequest struct {
	Object   Value
	Value    Value
	TypeID   uint32
	Field    uint32
	Kind     gc.StorageKind
	LiveRefs []Value // additional caller-known refs live across this helper safepoint
}

type ArrayLoadRequest struct {
	Array      Value
	Index      Value
	TypeID     uint32
	Kind       gc.StorageKind
	ResultType wasm.ValType
	LiveRefs   []Value // additional caller-known refs live across this helper safepoint
}

type ArrayStoreRequest struct {
	Array    Value
	Index    Value
	Value    Value
	TypeID   uint32
	Kind     gc.StorageKind
	LiveRefs []Value // additional caller-known refs live across this helper safepoint
}

type ArrayLenRequest struct {
	Array    Value
	LiveRefs []Value // additional caller-known refs live across this helper safepoint
}

type BarrierKind uint8

const (
	BarrierObjectField BarrierKind = iota + 1
	BarrierArrayElem
	BarrierGlobalSlot
	BarrierTableSlot
	BarrierRootSlot
)

type WriteBarrierRequest struct {
	Parent    Value // object or array ref when storing into object payloads
	Child     Value // stored ref; null/i31 filtering may be inline or helper-side
	Kind      BarrierKind
	SlotIndex uint32  // global/table/root slot index when Kind is slot-like
	LiveRefs  []Value // additional caller-known refs live across this helper safepoint
}

type BulkWriteBarrierRequest struct {
	Dst      Value
	Start    Value
	Length   Value
	Kind     BarrierKind
	LiveRefs []Value // additional caller-known refs live across this helper safepoint
}

type SafepointReason uint8

const (
	SafepointHelperCall SafepointReason = iota + 1
	SafepointWasmCall
	SafepointHostCall
	SafepointLoop
)

type SafepointRequest struct {
	LiveRefs []Value
	Reason   SafepointReason
}

func unsupported(policy, op string) error {
	return &UnsupportedHeapOpError{Policy: policy, Op: op}
}

func appendRefValues(dst []Value, vals []Value) []Value {
	for _, v := range vals {
		if v.IsRef() {
			dst = append(dst, v)
		}
	}
	return dst
}
