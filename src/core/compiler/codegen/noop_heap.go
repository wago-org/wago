package codegen

// NoopHeap is an allocator/GC policy for configurations that must not lower
// WasmGC heap operations. Barriers and safepoints are no-ops so non-GC modules
// can share plumbing, while allocation and object access fail clearly.
type NoopHeap struct {
	PolicyName string
	Layout     RefLayout
}

func (h NoopHeap) Name() string {
	if h.PolicyName != "" {
		return h.PolicyName
	}
	return "noop-heap"
}

func (h NoopHeap) RefLayout() RefLayout {
	if h.Layout.Bits != 0 {
		return h.Layout
	}
	return GCRefLayout
}

func (h NoopHeap) BeginModule(ModuleInfo) (ModuleHeapABI, error) {
	return noopModuleHeap{heap: h}, nil
}

type noopModuleHeap struct{ heap NoopHeap }

func (h noopModuleHeap) BeginFunc(FuncInfo) (FuncHeapABI, error) {
	return noopFuncHeap{heap: h.heap}, nil
}

type noopFuncHeap struct{ heap NoopHeap }

func (h noopFuncHeap) AllocObject(Emitter, AllocObjectRequest) (Value, error) {
	return Value{}, unsupported(h.heap.Name(), "alloc object")
}

func (h noopFuncHeap) AllocArray(Emitter, AllocArrayRequest) (Value, error) {
	return Value{}, unsupported(h.heap.Name(), "alloc array")
}

func (h noopFuncHeap) LoadField(Emitter, FieldLoadRequest) (Value, error) {
	return Value{}, unsupported(h.heap.Name(), "load field")
}

func (h noopFuncHeap) StoreField(Emitter, FieldStoreRequest) error {
	return unsupported(h.heap.Name(), "store field")
}

func (h noopFuncHeap) LoadArrayElem(Emitter, ArrayLoadRequest) (Value, error) {
	return Value{}, unsupported(h.heap.Name(), "load array elem")
}

func (h noopFuncHeap) StoreArrayElem(Emitter, ArrayStoreRequest) error {
	return unsupported(h.heap.Name(), "store array elem")
}

func (h noopFuncHeap) ArrayLen(Emitter, ArrayLenRequest) (Value, error) {
	return Value{}, unsupported(h.heap.Name(), "array len")
}

func (h noopFuncHeap) WriteBarrier(Emitter, WriteBarrierRequest) error { return nil }

func (h noopFuncHeap) BulkWriteBarrier(Emitter, BulkWriteBarrierRequest) error { return nil }

func (h noopFuncHeap) Safepoint(Emitter, SafepointRequest) error { return nil }

func (h noopFuncHeap) EndFunc(Emitter) error { return nil }
