package codegen

import (
	"errors"
	"slices"
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
	activeRoots int
	calls       []fakeRuntimeCall
	events      []string
	publishErr  error
	callErr     error
	unpubErr    error
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
	e.events = append(e.events, "call")
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
	e.events = append(e.events, "spill")
	e.spilled = append(e.spilled, append([]Value(nil), vals...))
	return PublishedRoots{Opaque: len(e.spilled)}, nil
}

func (e *fakeEmitter) PublishRoots(PublishedRoots) error {
	e.events = append(e.events, "publish")
	e.published++
	if e.publishErr != nil {
		return e.publishErr
	}
	e.activeRoots++
	return nil
}

func (e *fakeEmitter) UnpublishRoots(PublishedRoots) error {
	e.events = append(e.events, "unpublish")
	e.unpublished++
	if e.activeRoots > 0 {
		e.activeRoots--
	}
	return e.unpubErr
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

func TestHelperHeapAllocObjectPublishesDirectAndExtraLiveRefs(t *testing.T) {
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
	fieldRef := Value{Opaque: "field-ref", Type: wasm.AnyRef}
	fieldI32 := Value{Opaque: "field-i32", Type: wasm.I32}
	extraRef := Value{Opaque: "extra-ref", Type: wasm.ExternRef}
	extraI64 := Value{Opaque: "extra-i64", Type: wasm.I64}
	_, err = fh.AllocObject(emit, AllocObjectRequest{
		TypeID:     9,
		Fields:     []Value{fieldRef, fieldI32},
		ResultType: wasm.AnyRef,
		LiveRefs:   []Value{extraI64, extraRef},
	})
	if err != nil {
		t.Fatalf("AllocObject: %v", err)
	}
	if len(emit.spilled) != 1 {
		t.Fatalf("spilled calls = %d, want 1", len(emit.spilled))
	}
	wantRoots := []Value{fieldRef, extraRef}
	if !slices.Equal(emit.spilled[0], wantRoots) {
		t.Fatalf("spilled roots = %#v, want %#v", emit.spilled[0], wantRoots)
	}
	wantEvents := []string{"spill", "publish", "call", "unpublish"}
	if !slices.Equal(emit.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", emit.events, wantEvents)
	}
}

func TestHelperHeapPreservesDuplicateLiveRefsForOpaqueBackends(t *testing.T) {
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
	nonRef := Value{Opaque: "non-ref", Type: wasm.I32}
	if err := fh.StoreField(emit, FieldStoreRequest{
		Object: obj,
		Value:  child,
		TypeID: 5,
		Field:  1,
		Kind:   gc.StorageRef,
		LiveRefs: []Value{
			child,
			obj,
			nonRef,
			child,
		},
	}); err != nil {
		t.Fatalf("StoreField: %v", err)
	}
	// Value.Opaque is backend-owned, so HelperHeap does not infer equality or
	// deduplicate. It keeps direct refs first, then caller-provided live refs.
	wantRoots := []Value{obj, child, child, obj, child}
	if len(emit.spilled) != 1 || !slices.Equal(emit.spilled[0], wantRoots) {
		t.Fatalf("spilled roots = %#v, want %#v", emit.spilled, wantRoots)
	}
}

func TestHelperHeapAllocArrayPublishesInitAndExtraLiveRefs(t *testing.T) {
	rt := RuntimeFuncs{RuntimeAllocArray: {ID: RuntimeAllocArray, Name: "test.alloc_array"}}
	mh, err := (HelperHeap{Runtime: rt}).BeginModule(ModuleInfo{})
	if err != nil {
		t.Fatalf("BeginModule: %v", err)
	}
	fh, err := mh.BeginFunc(FuncInfo{})
	if err != nil {
		t.Fatalf("BeginFunc: %v", err)
	}
	emit := &fakeEmitter{}
	length := Value{Opaque: "len", Type: wasm.I32}
	init := Value{Opaque: "init", Type: wasm.AnyRef}
	extra := Value{Opaque: "extra", Type: wasm.FuncRef}
	nonRef := Value{Opaque: "non-ref", Type: wasm.I32}
	_, err = fh.AllocArray(emit, AllocArrayRequest{
		TypeID:     4,
		Length:     length,
		Init:       init,
		ResultType: wasm.AnyRef,
		LiveRefs:   []Value{extra, nonRef},
	})
	if err != nil {
		t.Fatalf("AllocArray: %v", err)
	}
	wantRoots := []Value{init, extra}
	if len(emit.spilled) != 1 || !slices.Equal(emit.spilled[0], wantRoots) {
		t.Fatalf("spilled roots = %#v, want %#v", emit.spilled, wantRoots)
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

func TestHelperHeapUnpublishesRootsAfterRuntimeHelperError(t *testing.T) {
	runtimeErr := errors.New("runtime helper failed")
	rt := RuntimeFuncs{RuntimeStoreArrayElem: {ID: RuntimeStoreArrayElem, Name: "test.store_array_elem"}}
	mh, err := (HelperHeap{Runtime: rt}).BeginModule(ModuleInfo{})
	if err != nil {
		t.Fatalf("BeginModule: %v", err)
	}
	fh, err := mh.BeginFunc(FuncInfo{})
	if err != nil {
		t.Fatalf("BeginFunc: %v", err)
	}
	emit := &fakeEmitter{callErr: runtimeErr}
	array := Value{Opaque: "array", Type: wasm.AnyRef}
	value := Value{Opaque: "value", Type: wasm.AnyRef}
	err = fh.StoreArrayElem(emit, ArrayStoreRequest{Array: array, Index: Value{Opaque: "index", Type: wasm.I32}, Value: value, TypeID: 3, Kind: gc.StorageRef})
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("StoreArrayElem err = %v, want %v", err, runtimeErr)
	}
	wantEvents := []string{"spill", "publish", "call", "unpublish"}
	if !slices.Equal(emit.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", emit.events, wantEvents)
	}
	if emit.activeRoots != 0 || emit.unpublished != 1 {
		t.Fatalf("active/unpublished roots = %d/%d, want 0/1", emit.activeRoots, emit.unpublished)
	}
}

func TestHelperHeapPublishFailureSkipsRuntimeCall(t *testing.T) {
	publishErr := errors.New("publish failed")
	rt := RuntimeFuncs{RuntimeArrayLen: {ID: RuntimeArrayLen, Name: "test.array_len"}}
	mh, err := (HelperHeap{Runtime: rt}).BeginModule(ModuleInfo{})
	if err != nil {
		t.Fatalf("BeginModule: %v", err)
	}
	fh, err := mh.BeginFunc(FuncInfo{})
	if err != nil {
		t.Fatalf("BeginFunc: %v", err)
	}
	emit := &fakeEmitter{publishErr: publishErr}
	_, err = fh.ArrayLen(emit, ArrayLenRequest{Array: Value{Opaque: "array", Type: wasm.AnyRef}})
	if !errors.Is(err, publishErr) {
		t.Fatalf("ArrayLen err = %v, want %v", err, publishErr)
	}
	wantEvents := []string{"spill", "publish"}
	if !slices.Equal(emit.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", emit.events, wantEvents)
	}
	if len(emit.calls) != 0 || emit.unpublished != 0 || emit.activeRoots != 0 {
		t.Fatalf("calls/unpublished/active = %d/%d/%d, want 0/0/0", len(emit.calls), emit.unpublished, emit.activeRoots)
	}
}

func TestHelperHeapUnpublishFailureIsSurfacedAndClearsRoots(t *testing.T) {
	unpublishErr := errors.New("unpublish failed")
	rt := RuntimeFuncs{RuntimeArrayLen: {ID: RuntimeArrayLen, Name: "test.array_len"}}
	mh, err := (HelperHeap{Runtime: rt}).BeginModule(ModuleInfo{})
	if err != nil {
		t.Fatalf("BeginModule: %v", err)
	}
	fh, err := mh.BeginFunc(FuncInfo{})
	if err != nil {
		t.Fatalf("BeginFunc: %v", err)
	}
	emit := &fakeEmitter{unpubErr: unpublishErr}
	_, err = fh.ArrayLen(emit, ArrayLenRequest{Array: Value{Opaque: "array", Type: wasm.AnyRef}})
	if !errors.Is(err, unpublishErr) {
		t.Fatalf("ArrayLen err = %v, want %v", err, unpublishErr)
	}
	wantEvents := []string{"spill", "publish", "call", "unpublish"}
	if !slices.Equal(emit.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", emit.events, wantEvents)
	}
	if emit.activeRoots != 0 || emit.unpublished != 1 {
		t.Fatalf("active/unpublished roots = %d/%d, want 0/1", emit.activeRoots, emit.unpublished)
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
