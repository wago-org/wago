package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
)

func exceptionPayloadRootKind(m *wasm.Module, typ wasm.ValType) (nativeabi.RootKind, bool) {
	if typ.Kind != wasm.ValRef {
		return 0, false
	}
	heap := typ.Ref.Heap
	switch heap.Kind {
	case wasm.HeapAbs:
		if heap.Abs == wasm.HeapFunc || heap.Abs == wasm.HeapNoFunc {
			return nativeabi.RootFuncRef, true
		}
		return nativeabi.RootGCRef, true
	case wasm.HeapTypeIndex:
		if ft, ok := m.ResolvedTypeFunc(heap.Type.Index); ok && ft != nil {
			return nativeabi.RootFuncRef, true
		}
		return nativeabi.RootGCRef, true
	case wasm.HeapDefType:
		// DefType-backed references are recursive-group-local decoder products.
		// Conservatively classify them as collector refs unless a later resolved
		// absolute type index proves a function component.
		return nativeabi.RootGCRef, true
	default:
		return 0, false
	}
}

func catchAllPayloadRootKinds(m *wasm.Module) ([2]nativeabi.RootKind, [2]bool, error) {
	var kinds [2]nativeabi.RootKind
	var roots, scalars [2]bool
	for tag := uint32(0); tag < uint32(m.TagCount()); tag++ {
		tagType, ok := moduleTagType(m, tag)
		if !ok {
			return kinds, roots, fmt.Errorf("tag %d is unavailable", tag)
		}
		ft, ok := m.ResolvedTypeFunc(tagType.Type.Index)
		if !ok || len(ft.Params) > len(kinds) {
			return kinds, roots, fmt.Errorf("tag %d payload is unsupported", tag)
		}
		for payload, typ := range ft.Params {
			kind, isReference := exceptionPayloadRootKind(m, typ)
			if !isReference {
				scalars[payload] = true
				if roots[payload] {
					return kinds, roots, fmt.Errorf("payload %d mixes scalar and reference ownership", payload)
				}
				continue
			}
			if scalars[payload] {
				return kinds, roots, fmt.Errorf("payload %d mixes scalar and reference ownership", payload)
			}
			if roots[payload] && kinds[payload] != kind {
				return kinds, roots, fmt.Errorf("payload %d mixes GC and funcref ownership", payload)
			}
			kinds[payload], roots[payload] = kind, true
		}
	}
	return kinds, roots, nil
}

// BuildExceptionRootMaps describes reference payloads copied into the four fixed
// exception-root records. It does not enable GC payload execution: callers must
// still provide safepoint publication, root initialization, barriers/remark, and
// funcref producer retention before trusting these maps at runtime.
func BuildExceptionRootMaps(m *wasm.Module) ([]nativeabi.FunctionRootMap, error) {
	if m == nil || m.TagCount() == 0 {
		return nil, nil
	}
	maps := make([]nativeabi.FunctionRootMap, 0, len(m.Code))
	for function := range m.Code {
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return nil, fmt.Errorf("exception root map function %d type is unavailable", function)
		}
		nLocals, err := countLocals(ft.Params, m.Code[function].Locals)
		if err != nil {
			return nil, fmt.Errorf("exception root map function %d locals: %w", function, err)
		}
		frameBytes := frameHdrBytes + 8*nLocals + (maxEHTryRecords*ehRecordSlots+maxEHRootRecords*ehRootSlots)*8
		rootCount := 0
		var slots []nativeabi.RootSlot
		var catchAllKinds [2]nativeabi.RootKind
		var catchAllRoots [2]bool
		catchAllReady := false
		r := wasm.NewReader(m.Code[function].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return nil, err
			}
			if op != 0x1f {
				if _, err := wasm.ClassifyInstructionImmediate(r, op); err != nil {
					return nil, fmt.Errorf("exception root map function %d: %w", function, err)
				}
				continue
			}
			if _, err := r.S33(); err != nil {
				return nil, err
			}
			n, err := r.U32()
			if err != nil {
				return nil, err
			}
			for i := uint32(0); i < n; i++ {
				kindByte, err := r.Byte()
				if err != nil {
					return nil, err
				}
				kind := wasm.CatchKind(kindByte)
				var tag uint32
				if kind == wasm.CatchTag || kind == wasm.CatchRef {
					tag, err = r.U32()
					if err != nil {
						return nil, err
					}
				}
				if _, err := r.U32(); err != nil {
					return nil, err
				}
				if kind != wasm.CatchRef && kind != wasm.CatchAllRef {
					continue
				}
				if rootCount >= maxEHRootRecords {
					return nil, fmt.Errorf("exception root map function %d exceeds %d fixed roots", function, maxEHRootRecords)
				}
				rootOff := frameHdrBytes + 8*nLocals + maxEHTryRecords*ehRecordSlots*8 + rootCount*ehRootSlots*8
				if kind == wasm.CatchRef {
					tagType, ok := moduleTagType(m, tag)
					if !ok {
						return nil, fmt.Errorf("exception root map function %d tag %d is unavailable", function, tag)
					}
					tagFunc, ok := m.ResolvedTypeFunc(tagType.Type.Index)
					if !ok || len(tagFunc.Params) > 2 {
						return nil, fmt.Errorf("exception root map function %d tag %d payload is unsupported", function, tag)
					}
					for payload, typ := range tagFunc.Params {
						rootKind, isReference := exceptionPayloadRootKind(m, typ)
						if isReference {
							slots = append(slots, nativeabi.RootSlot{Offset: uint32(rootOff + 8 + payload*8), Kind: rootKind})
						}
					}
				} else {
					if !catchAllReady {
						var err error
						catchAllKinds, catchAllRoots, err = catchAllPayloadRootKinds(m)
						if err != nil {
							return nil, fmt.Errorf("exception root map function %d catch_all_ref: %w", function, err)
						}
						catchAllReady = true
					}
					for payload, isReference := range catchAllRoots {
						if isReference {
							slots = append(slots, nativeabi.RootSlot{Offset: uint32(rootOff + 8 + payload*8), Kind: catchAllKinds[payload]})
						}
					}
				}
				rootCount++
			}
		}
		if len(slots) != 0 {
			maps = append(maps, nativeabi.FunctionRootMap{LocalFunction: uint32(function), FrameBytes: uint32(frameBytes), Slots: slots})
		}
	}
	if err := nativeabi.ValidateRootMaps(maps, len(m.Code)); err != nil {
		return nil, err
	}
	return maps, nil
}
