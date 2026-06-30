package codegen

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

var _ HeapABI = NoopHeap{}
var _ HeapABI = HelperHeap{}

type fakeEmitter struct {
	spilled     [][]Value
	published   int
	unpublished int
	calls       []fakeRuntimeCall
	callErr     error
}

type fakeRuntimeCall struct {
	fn      RuntimeFunc
	args    []Value
	results []wasm.ValType
}

func (e *fakeEmitter) ConstI32(v uint32) Value                   { return Value{Opaque: v, Type: wasm.I32} }
func (e *fakeEmitter) ConstI64(v uint64) Value                   { return Value{Opaque: v, Type: wasm.I64} }
func (e *fakeEmitter) Load(Address, wasm.ValType) (Value, error) { return Value{}, nil }
func (e *fakeEmitter) Store(Address, Value, wasm.ValType) error  { return nil }
func (e *fakeEmitter) Trap(TrapCode) error                       { return nil }

func (e *fakeEmitter) CallRuntime(fn RuntimeFunc, args []Value, results []wasm.ValType) ([]Value, error) {
	e.calls = append(e.calls, fakeRuntimeCall{fn: fn, args: append([]Value(nil), args...), results: append([]wasm.ValType(nil), results...)})
	if e.callErr != nil {
		return nil, e.callErr
	}
	out := make([]Value, len(results))
	for i, t := range results {
		out[i] = Value{Opaque: fn.Name, Type: t}
	}
	return out, nil
}

func (e *fakeEmitter) SpillLiveRefs(vals []Value) (PublishedRoots, error) {
	e.spilled = append(e.spilled, append([]Value(nil), vals...))
	return PublishedRoots{Opaque: len(e.spilled)}, nil
}

func (e *fakeEmitter) PublishRoots(PublishedRoots) error {
	e.published++
	return nil
}

func (e *fakeEmitter) UnpublishRoots(PublishedRoots) error {
	e.unpublished++
	return nil
}

func TestNoopHeapRejectsHeapOpsButAllowsBarriers(t *testing.T) {
	mh, err := (NoopHeap{}).BeginModule(ModuleInfo{})
	if err != nil {
		t.Fatalf("BeginModule: %v", err)
	}
	fh, err := mh.BeginFunc(FuncInfo{})
	if err != nil {
		t.Fatalf("BeginFunc: %v", err)
	}
	if _, err := fh.AllocObject(&fakeEmitter{}, AllocObjectRequest{}); !errors.Is(err, ErrUnsupportedHeapOp) {
		t.Fatalf("AllocObject err = %v, want ErrUnsupportedHeapOp", err)
	}
	if err := fh.WriteBarrier(&fakeEmitter{}, WriteBarrierRequest{}); err != nil {
		t.Fatalf("WriteBarrier noop err = %v", err)
	}
	if err := fh.Safepoint(&fakeEmitter{}, SafepointRequest{}); err != nil {
		t.Fatalf("Safepoint noop err = %v", err)
	}
}

func TestHelperHeapAllocObjectPublishesOnlyRefRoots(t *testing.T) {
	rt := RuntimeFuncs{RuntimeAllocObject: {ID: RuntimeAllocObject, Name: "test.alloc_object"}}
	mh, err := (HelperHeap{Runtime: rt}).BeginModule(ModuleInfo{})
	if err != nil {
		t.Fatalf("BeginModule: %v", err)
	}
	fh, err := mh.BeginFunc(FuncInfo{})
	if err != nil {
		t.Fatalf("BeginFunc: %v", err)
	}
	emit := &fakeEmitter{}
	refField := Value{Opaque: "ref", Type: wasm.AnyRef}
	intField := Value{Opaque: "i32", Type: wasm.I32}
	got, err := fh.AllocObject(emit, AllocObjectRequest{TypeID: 7, Fields: []Value{refField, intField}, ResultType: wasm.AnyRef})
	if err != nil {
		t.Fatalf("AllocObject: %v", err)
	}
	if got.Type.Kind != wasm.ValRef {
		t.Fatalf("AllocObject result type = %s, want ref", got.Type)
	}
	if len(emit.calls) != 1 {
		t.Fatalf("runtime calls = %d, want 1", len(emit.calls))
	}
	if emit.calls[0].fn.Name != "test.alloc_object" {
		t.Fatalf("runtime func = %q", emit.calls[0].fn.Name)
	}
	if len(emit.calls[0].args) != 3 {
		t.Fatalf("runtime args = %d, want type id + 2 fields", len(emit.calls[0].args))
	}
	if len(emit.spilled) != 1 || len(emit.spilled[0]) != 1 || emit.spilled[0][0].Opaque != "ref" {
		t.Fatalf("spilled roots = %#v, want only ref field", emit.spilled)
	}
	if emit.published != 1 || emit.unpublished != 1 {
		t.Fatalf("publish/unpublish = %d/%d, want 1/1", emit.published, emit.unpublished)
	}
}

func TestHelperHeapStoreFieldUsesStoreHelperAndRootsObjectAndRefValue(t *testing.T) {
	rt := RuntimeFuncs{RuntimeStoreField: {ID: RuntimeStoreField, Name: "test.store_field"}}
	mh, err := (HelperHeap{Runtime: rt}).BeginModule(ModuleInfo{})
	if err != nil {
		t.Fatalf("BeginModule: %v", err)
	}
	fh, err := mh.BeginFunc(FuncInfo{})
	if err != nil {
		t.Fatalf("BeginFunc: %v", err)
	}
	emit := &fakeEmitter{}
	obj := Value{Opaque: "obj", Type: wasm.AnyRef}
	child := Value{Opaque: "child", Type: wasm.AnyRef}
	if err := fh.StoreField(emit, FieldStoreRequest{Object: obj, Value: child, TypeID: 3, Field: 2, Kind: gc.StorageRef}); err != nil {
		t.Fatalf("StoreField: %v", err)
	}
	if len(emit.calls) != 1 || emit.calls[0].fn.Name != "test.store_field" {
		t.Fatalf("calls = %#v", emit.calls)
	}
	if len(emit.spilled) != 1 || len(emit.spilled[0]) != 2 {
		t.Fatalf("spilled roots = %#v, want object and child", emit.spilled)
	}
}

func TestHelperHeapMissingRuntimeHelperIsUnsupported(t *testing.T) {
	mh, err := (HelperHeap{Runtime: RuntimeFuncs{}}).BeginModule(ModuleInfo{})
	if err != nil {
		t.Fatalf("BeginModule: %v", err)
	}
	fh, err := mh.BeginFunc(FuncInfo{})
	if err != nil {
		t.Fatalf("BeginFunc: %v", err)
	}
	_, err = fh.ArrayLen(&fakeEmitter{}, ArrayLenRequest{Array: Value{Opaque: "array", Type: wasm.AnyRef}})
	if !errors.Is(err, ErrUnsupportedHeapOp) {
		t.Fatalf("ArrayLen err = %v, want ErrUnsupportedHeapOp", err)
	}
}
