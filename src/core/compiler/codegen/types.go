// Package codegen defines backend-neutral compiler contracts shared by wasm and
// IR lowering paths.
//
// The package intentionally contains interfaces and small value objects only. It
// must not know about amd64 registers, direct-backend operand stack entries, or
// IR value ids; backends adapt those details behind Emitter and Value.
package codegen

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// Object is the common shape of a compiled native-code blob.
type Object struct {
	Code  []byte
	Entry []int
}

// Backend compiles a module-like input to native code. The direct amd64 backend
// can instantiate M as *wasm.Module; the IR backend can instantiate M as
// *ir.Module without forcing the heap/GC API itself to mention IR types.
type Backend[M any] interface {
	Name() string
	CompileModule(m M, opts Options) (*Object, error)
}

// Options are shared code-generation dependencies selected by the caller after
// frontend validation and runtime configuration normalization.
type Options struct {
	Runtime RuntimeABI
	Heap    HeapABI
}

// Value is an opaque backend-owned value handle paired with its wasm type.
//
// Heap policies must not inspect Opaque. They pass Value tokens back to Emitter
// methods, letting each backend decide whether the value is a register, spill
// slot, local, call slot, IR value, materialized constant, or temporary.
type Value struct {
	Opaque any
	Type   wasm.ValType
}

// IsRef reports whether v is a wasm reference value and therefore may need root
// publication across allocating runtime calls.
func (v Value) IsRef() bool { return v.Type.Kind == wasm.ValRef }

// RefLayout documents the compact guest reference representation used by a heap
// policy. The current runtime/gc.Ref layout is 32-bit: null is 0, i31 immediates
// have low bit 1, object handles have low bit 0 and are non-zero.
type RefLayout struct {
	Bits           uint8
	Null           uint64
	I31TagMask     uint64
	I31TagValue    uint64
	ObjectTagMask  uint64
	ObjectTagValue uint64
}

// GCRefLayout is the runtime/gc.Ref layout used by the current WasmGC runtime.
var GCRefLayout = RefLayout{
	Bits:           32,
	Null:           0,
	I31TagMask:     1,
	I31TagValue:    1,
	ObjectTagMask:  1,
	ObjectTagValue: 0,
}

// ModuleInfo is the heap-relevant module metadata passed to HeapABI.
type ModuleInfo struct {
	Features    Features
	GCTypeDescs []gc.TypeDesc
}

// FuncInfo is the heap-relevant function metadata passed to ModuleHeapABI.
type FuncInfo struct {
	Index uint32
	Sig   wasm.FuncType
}

// Features records codegen-relevant optional capabilities without tying the
// shared heap ABI to a particular frontend representation.
type Features struct {
	V128 bool
	GC   bool
}

// Address is an abstract backend address. It is deliberately small: backends can
// reject address forms they cannot materialize.
type Address struct {
	Base   Value
	Index  Value
	Scale  uint8
	Offset int64
}

// TrapCode is the codegen-facing trap identifier. Backends map it to their
// runtime trap slot encoding.
type TrapCode uint32

// RuntimeFunc identifies a runtime helper callable by generated code.
type RuntimeFunc struct {
	ID   RuntimeFuncID
	Name string
}

// RuntimeFuncID names helper-call slots used by helper-call-first heap lowering.
type RuntimeFuncID uint16

const (
	RuntimeAllocObject RuntimeFuncID = iota + 1
	RuntimeAllocArray
	RuntimeLoadField
	RuntimeStoreField
	RuntimeLoadArrayElem
	RuntimeStoreArrayElem
	RuntimeArrayLen
	RuntimeWriteBarrier
	RuntimeBulkWriteBarrier
	RuntimeSafepoint
)

func (id RuntimeFuncID) String() string {
	switch id {
	case RuntimeAllocObject:
		return "gc.alloc_object"
	case RuntimeAllocArray:
		return "gc.alloc_array"
	case RuntimeLoadField:
		return "gc.load_field"
	case RuntimeStoreField:
		return "gc.store_field"
	case RuntimeLoadArrayElem:
		return "gc.load_array_elem"
	case RuntimeStoreArrayElem:
		return "gc.store_array_elem"
	case RuntimeArrayLen:
		return "gc.array_len"
	case RuntimeWriteBarrier:
		return "gc.write_barrier"
	case RuntimeBulkWriteBarrier:
		return "gc.bulk_write_barrier"
	case RuntimeSafepoint:
		return "gc.safepoint"
	default:
		return fmt.Sprintf("runtime_func(%d)", uint16(id))
	}
}

// RuntimeABI resolves codegen helper names to backend-callable runtime functions.
type RuntimeABI interface {
	RuntimeFunc(RuntimeFuncID) (RuntimeFunc, bool)
}

// RuntimeFuncs is a small map-backed RuntimeABI useful for tests and for early
// helper-call-first integration before the runtime ABI is finalized.
type RuntimeFuncs map[RuntimeFuncID]RuntimeFunc

func (r RuntimeFuncs) RuntimeFunc(id RuntimeFuncID) (RuntimeFunc, bool) {
	fn, ok := r[id]
	if !ok && r != nil {
		return RuntimeFunc{}, false
	}
	if ok {
		return fn, true
	}
	return RuntimeFunc{ID: id, Name: id.String()}, true
}

// PublishedRoots is an opaque backend handle for roots published around helper
// calls. It may represent stack slots, side-table entries, or a no-op token.
type PublishedRoots struct{ Opaque any }

// Emitter is implemented by concrete backends. Heap policies use it to ask the
// backend to materialize constants, memory operations, helper calls, traps, and
// exact root publication without learning backend internals.
type Emitter interface {
	ConstI32(uint32) Value
	ConstI64(uint64) Value

	Load(Address, wasm.ValType) (Value, error)
	Store(Address, Value, wasm.ValType) error

	CallRuntime(RuntimeFunc, []Value, []wasm.ValType) ([]Value, error)
	Trap(TrapCode) error

	// SpillLiveRefs prepares backend-owned storage for the supplied refs but does
	// not publish it to the runtime collector.
	SpillLiveRefs([]Value) (PublishedRoots, error)
	// PublishRoots makes spilled roots visible to runtime collection. Publication
	// must be all-or-nothing: if an error is returned, no roots are considered
	// published and HelperHeap will not call UnpublishRoots.
	PublishRoots(PublishedRoots) error
	// UnpublishRoots hides a successfully published root set. If PublishRoots
	// succeeds, HelperHeap calls UnpublishRoots exactly once even when CallRuntime
	// fails. When CallRuntime and UnpublishRoots both fail, HelperHeap returns the
	// CallRuntime error; when only UnpublishRoots fails, that error is returned.
	UnpublishRoots(PublishedRoots) error
}
