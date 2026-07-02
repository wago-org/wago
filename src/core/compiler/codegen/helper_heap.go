package codegen

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// HelperHeap is a helper-call-first heap policy. It emits all WasmGC heap
// operations through RuntimeABI helpers, publishing exact roots around helper
// calls that may allocate, collect, or otherwise need stable ref arguments.
//
// This is intentionally conservative and matches the current runtime ABI note:
// generated code must not cache raw WasmGC payload pointers while heap arenas can
// be backed by movable Go byte slices.
type HelperHeap struct {
	PolicyName string
	Runtime    RuntimeABI
	Layout     RefLayout
}

func (h HelperHeap) Name() string {
	if h.PolicyName != "" {
		return h.PolicyName
	}
	return "helper-heap"
}

func (h HelperHeap) RefLayout() RefLayout {
	if h.Layout.Bits != 0 {
		return h.Layout
	}
	return GCRefLayout
}

func (h HelperHeap) BeginModule(info ModuleInfo) (ModuleHeapABI, error) {
	return helperModuleHeap{heap: h, info: info}, nil
}

type helperModuleHeap struct {
	heap HelperHeap
	info ModuleInfo
}

func (h helperModuleHeap) BeginFunc(info FuncInfo) (FuncHeapABI, error) {
	return helperFuncHeap{heap: h.heap, module: h.info, fn: info}, nil
}

type helperFuncHeap struct {
	heap   HelperHeap
	module ModuleInfo
	fn     FuncInfo
}

func (h helperFuncHeap) AllocObject(e Emitter, req AllocObjectRequest) (Value, error) {
	if e == nil {
		return Value{}, nilEmitterError(RuntimeAllocObject)
	}
	args := make([]Value, 0, 1+len(req.Fields))
	args = append(args, e.ConstI32(req.TypeID))
	args = append(args, req.Fields...)
	results, err := h.helperCall(e, RuntimeAllocObject, args, []wasm.ValType{req.ResultType}, helperLiveRefs(req.LiveRefs, req.Fields...))
	if err != nil {
		return Value{}, err
	}
	return singleRuntimeResult(RuntimeAllocObject, results)
}

func (h helperFuncHeap) AllocArray(e Emitter, req AllocArrayRequest) (Value, error) {
	if e == nil {
		return Value{}, nilEmitterError(RuntimeAllocArray)
	}
	args := []Value{e.ConstI32(req.TypeID), req.Length, req.Init}
	results, err := h.helperCall(e, RuntimeAllocArray, args, []wasm.ValType{req.ResultType}, helperLiveRefs(req.LiveRefs, req.Init))
	if err != nil {
		return Value{}, err
	}
	return singleRuntimeResult(RuntimeAllocArray, results)
}

func (h helperFuncHeap) LoadField(e Emitter, req FieldLoadRequest) (Value, error) {
	if e == nil {
		return Value{}, nilEmitterError(RuntimeLoadField)
	}
	args := []Value{req.Object, e.ConstI32(req.TypeID), e.ConstI32(req.Field), e.ConstI32(uint32(req.Kind))}
	results, err := h.helperCall(e, RuntimeLoadField, args, []wasm.ValType{req.ResultType}, helperLiveRefs(req.LiveRefs, req.Object))
	if err != nil {
		return Value{}, err
	}
	return singleRuntimeResult(RuntimeLoadField, results)
}

func (h helperFuncHeap) StoreField(e Emitter, req FieldStoreRequest) error {
	if e == nil {
		return nilEmitterError(RuntimeStoreField)
	}
	args := []Value{req.Object, e.ConstI32(req.TypeID), e.ConstI32(req.Field), e.ConstI32(uint32(req.Kind)), req.Value}
	_, err := h.helperCall(e, RuntimeStoreField, args, nil, helperLiveRefs(req.LiveRefs, req.Object, req.Value))
	return err
}

func (h helperFuncHeap) LoadArrayElem(e Emitter, req ArrayLoadRequest) (Value, error) {
	if e == nil {
		return Value{}, nilEmitterError(RuntimeLoadArrayElem)
	}
	args := []Value{req.Array, req.Index, e.ConstI32(req.TypeID), e.ConstI32(uint32(req.Kind))}
	results, err := h.helperCall(e, RuntimeLoadArrayElem, args, []wasm.ValType{req.ResultType}, helperLiveRefs(req.LiveRefs, req.Array))
	if err != nil {
		return Value{}, err
	}
	return singleRuntimeResult(RuntimeLoadArrayElem, results)
}

func (h helperFuncHeap) StoreArrayElem(e Emitter, req ArrayStoreRequest) error {
	if e == nil {
		return nilEmitterError(RuntimeStoreArrayElem)
	}
	args := []Value{req.Array, req.Index, e.ConstI32(req.TypeID), e.ConstI32(uint32(req.Kind)), req.Value}
	_, err := h.helperCall(e, RuntimeStoreArrayElem, args, nil, helperLiveRefs(req.LiveRefs, req.Array, req.Value))
	return err
}

func (h helperFuncHeap) ArrayLen(e Emitter, req ArrayLenRequest) (Value, error) {
	if e == nil {
		return Value{}, nilEmitterError(RuntimeArrayLen)
	}
	results, err := h.helperCall(e, RuntimeArrayLen, []Value{req.Array}, []wasm.ValType{wasm.I32}, helperLiveRefs(req.LiveRefs, req.Array))
	if err != nil {
		return Value{}, err
	}
	return singleRuntimeResult(RuntimeArrayLen, results)
}

func (h helperFuncHeap) WriteBarrier(e Emitter, req WriteBarrierRequest) error {
	if e == nil {
		return nilEmitterError(RuntimeWriteBarrier)
	}
	args := []Value{e.ConstI32(uint32(req.Kind)), e.ConstI32(req.SlotIndex), req.Parent, req.Child}
	_, err := h.helperCall(e, RuntimeWriteBarrier, args, nil, helperLiveRefs(req.LiveRefs, req.Parent, req.Child))
	return err
}

func (h helperFuncHeap) BulkWriteBarrier(e Emitter, req BulkWriteBarrierRequest) error {
	if e == nil {
		return nilEmitterError(RuntimeBulkWriteBarrier)
	}
	args := []Value{e.ConstI32(uint32(req.Kind)), req.Dst, req.Start, req.Length}
	_, err := h.helperCall(e, RuntimeBulkWriteBarrier, args, nil, helperLiveRefs(req.LiveRefs, req.Dst))
	return err
}

func (h helperFuncHeap) Safepoint(e Emitter, req SafepointRequest) error {
	if e == nil {
		return nilEmitterError(RuntimeSafepoint)
	}
	_, err := h.helperCall(e, RuntimeSafepoint, []Value{e.ConstI32(uint32(req.Reason))}, nil, helperLiveRefs(req.LiveRefs))
	return err
}

func (h helperFuncHeap) EndFunc(Emitter) error { return nil }

func helperLiveRefs(extra []Value, direct ...Value) []Value {
	live := make([]Value, 0, len(direct)+len(extra))
	live = appendRefValues(live, direct)
	live = appendRefValues(live, extra)
	return live
}

func (h helperFuncHeap) helperCall(e Emitter, id RuntimeFuncID, args []Value, results []wasm.ValType, liveRefs []Value) ([]Value, error) {
	rt := h.heap.Runtime
	if rt == nil {
		rt = RuntimeFuncs(nil)
	}
	fn, ok := rt.RuntimeFunc(id)
	if !ok {
		return nil, unsupported(h.heap.Name(), id.String())
	}
	roots, err := e.SpillLiveRefs(liveRefs)
	if err != nil {
		return nil, err
	}
	if err := e.PublishRoots(roots); err != nil {
		return nil, err
	}
	callResults, callErr := e.CallRuntime(fn, args, results)
	unpublishErr := e.UnpublishRoots(roots)
	if callErr != nil {
		return nil, callErr
	}
	if unpublishErr != nil {
		return nil, unpublishErr
	}
	return callResults, nil
}

func nilEmitterError(id RuntimeFuncID) error {
	return fmt.Errorf("codegen: nil emitter for %s", id)
}

func singleRuntimeResult(id RuntimeFuncID, results []Value) (Value, error) {
	if len(results) != 1 {
		return Value{}, fmt.Errorf("codegen: %s returned %d results, want 1", id, len(results))
	}
	return results[0], nil
}
