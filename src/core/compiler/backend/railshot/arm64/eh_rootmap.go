//go:build arm64

package arm64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
)

// catchAllPayloadRootKinds merges every tag's lane ownership for a
// catch_all_ref slot, which may hold any of them. Identity lanes must agree on
// scalar versus funcref. GC lanes are only ever references or zero, and the
// slot may hold a same-domain foreign exception whose tag this module never
// declares, so every GC lane is reported.
func catchAllPayloadRootKinds(m *wasm.Module) ([shared.EHPayloadLanes]nativeabi.RootKind, [shared.EHPayloadLanes]bool, error) {
	var kinds [shared.EHPayloadLanes]nativeabi.RootKind
	var roots, scalars [shared.EHPayloadLanes]bool
	for tag := uint32(0); tag < uint32(m.TagCount()); tag++ {
		tagType, ok := moduleTagType(m, tag)
		if !ok {
			return kinds, roots, fmt.Errorf("tag %d is unavailable", tag)
		}
		var ft wasm.CompType
		if !m.ResolveTypeFunc(tagType.Type.Index, &ft) || len(ft.Params) > ehMaxPayloadWords {
			return kinds, roots, fmt.Errorf("tag %d payload is unsupported", tag)
		}
		for payload, typ := range ft.Params {
			lane := shared.EHPayloadLane(m, typ, payload)
			kind, isReference := shared.EHPayloadRootKind(m, typ)
			if !isReference {
				scalars[lane] = true
				if roots[lane] {
					return kinds, roots, fmt.Errorf("payload %d mixes scalar and reference ownership", payload)
				}
				continue
			}
			if scalars[lane] {
				return kinds, roots, fmt.Errorf("payload %d mixes scalar and reference ownership", payload)
			}
			kinds[lane], roots[lane] = kind, true
		}
	}
	for lane := shared.EHGCLaneBase; lane < shared.EHPayloadLanes; lane++ {
		kinds[lane], roots[lane] = nativeabi.RootGCRef, true
	}
	return kinds, roots, nil
}

// BuildExceptionRootMaps describes reference payloads copied into each
// function's exception root slots. GC lanes become fixed collector roots of the
// frame; funcref lanes describe identities whose producers the instance keeps
// alive and are not scanned by the collector.
func BuildExceptionRootMaps(m *wasm.Module) ([]nativeabi.FunctionRootMap, error) {
	if m == nil || m.TagCount() == 0 {
		return nil, nil
	}
	maps := make([]nativeabi.FunctionRootMap, 0, len(m.Code))
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for function := range m.Code {
		ft, ok := m.LocalFuncType(function)
		if !ok {
			return nil, fmt.Errorf("exception root map function %d type is unavailable", function)
		}
		if _, err := countLocals(ft.Params, m.Code[function].Locals); err != nil {
			return nil, fmt.Errorf("exception root map function %d locals: %w", function, err)
		}
		nLocalSlots := 0
		for _, typ := range ft.Params {
			nLocalSlots += mtOf(typ).stackSlots()
		}
		for _, run := range m.Code[function].Locals.Runs {
			nLocalSlots += int(run.Count) * mtOf(run.Type).stackSlots()
		}
		shape := shared.EHFrameShape{TryRecords: legacyEHTryRecords, RootRecords: legacyEHRootRecords}
		if len(m.Code[function].BodyBytes) != 0 {
			var err error
			shape, err = shared.ScanEHFrameShape(&classifier, m.Code[function].BodyBytes)
			if err != nil {
				return nil, fmt.Errorf("exception root map function %d: %w", function, err)
			}
		}
		frameBytes := frameHdrBytes + 8*nLocalSlots + (shape.TryRecords*ehRecordSlots+shape.RootRecords*ehRootSlots)*8
		rootCount := 0
		var slots []nativeabi.RootSlot
		var catchAllKinds [shared.EHPayloadLanes]nativeabi.RootKind
		var catchAllRoots [shared.EHPayloadLanes]bool
		catchAllReady := false
		r := wasm.NewReader(m.Code[function].BodyBytes)
		var imm wasm.InstructionImmediate
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return nil, err
			}
			if op != 0x1f {
				if err := classifier.ClassifyInto(r, op, &imm); err != nil {
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
				if rootCount >= shape.RootRecords {
					return nil, fmt.Errorf("exception root map function %d exceeds %d reserved roots", function, shape.RootRecords)
				}
				rootOff := frameHdrBytes + 8*nLocalSlots + shape.TryRecords*ehRecordSlots*8 + rootCount*ehRootSlots*8
				if kind == wasm.CatchRef {
					tagType, ok := moduleTagType(m, tag)
					if !ok {
						return nil, fmt.Errorf("exception root map function %d tag %d is unavailable", function, tag)
					}
					var tagFunc wasm.CompType
					if !m.ResolveTypeFunc(tagType.Type.Index, &tagFunc) || len(tagFunc.Params) > ehMaxPayloadWords {
						return nil, fmt.Errorf("exception root map function %d tag %d payload is unsupported", function, tag)
					}
					for payload, typ := range tagFunc.Params {
						rootKind, isReference := shared.EHPayloadRootKind(m, typ)
						if isReference {
							lane := shared.EHPayloadLane(m, typ, payload)
							slots = append(slots, nativeabi.RootSlot{Offset: uint32(rootOff + 8 + lane*8), Kind: rootKind})
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
					for lane, isReference := range catchAllRoots {
						if isReference {
							slots = append(slots, nativeabi.RootSlot{Offset: uint32(rootOff + 8 + lane*8), Kind: catchAllKinds[lane]})
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
