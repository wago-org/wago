package frontend

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// BuildGCTypeDescs lowers decoded Wasm GC recursive type groups into runtime GC
// descriptors. The returned slice is indexed by flattened wasm.TypeIdx.Index.
func BuildGCTypeDescs(m *wasm.Module) ([]gc.TypeDesc, error) {
	if m == nil {
		return nil, fmt.Errorf("frontend: nil wasm module")
	}
	return LowerGCTypeDescs(m.Types)
}

// LowerGCTypeDescs flattens recursive groups in decoder/validator order and
// returns descriptors indexed by the same flattened wasm.TypeIdx.Index values.
// Function types get gc.KindFunc sentinels so later struct/array indexes are not
// shifted. Field mutability affects validation/codegen, not GC layout or scan
// reachability, so it is intentionally not represented in gc.TypeDesc.
func LowerGCTypeDescs(types []wasm.RecType) ([]gc.TypeDesc, error) {
	flat := flattenGCTypes(types)
	descs := make([]gc.TypeDesc, len(flat))
	for i, st := range flat {
		id := gc.TypeID(i)
		var d gc.TypeDesc
		var err error
		switch st.Comp.Kind {
		case wasm.CompFunc:
			d = gc.TypeDesc{ID: id, Kind: gc.KindFunc, Final: st.Final}
		case wasm.CompStruct:
			fields := make([]gc.StorageKind, len(st.Comp.Fields))
			for j, f := range st.Comp.Fields {
				fields[j], err = lowerGCStorage(f.Storage, len(flat))
				if err != nil {
					return nil, fmt.Errorf("frontend: type %d field %d: %w", i, j, err)
				}
			}
			d, err = gc.NewStructDesc(id, fields)
			if err != nil {
				return nil, fmt.Errorf("frontend: type %d struct: %w", i, err)
			}
			d.Final = st.Final
		case wasm.CompArray:
			elem, err := lowerGCStorage(st.Comp.Array.Storage, len(flat))
			if err != nil {
				return nil, fmt.Errorf("frontend: type %d array: %w", i, err)
			}
			d, err = gc.NewArrayDesc(id, elem)
			if err != nil {
				return nil, fmt.Errorf("frontend: type %d array: %w", i, err)
			}
			d.Final = st.Final
		default:
			return nil, fmt.Errorf("frontend: type %d has unsupported component kind %d", i, st.Comp.Kind)
		}
		if len(st.Supers) > 0 {
			if len(st.Supers) > 1 {
				return nil, fmt.Errorf("frontend: type %d has multiple supers; runtime descriptor stores one", i)
			}
			super := st.Supers[0]
			if super.Rec || super.Index >= uint32(len(flat)) {
				return nil, fmt.Errorf("frontend: type %d has invalid super type index %d", i, super.Index)
			}
			d.Super = gc.TypeID(super.Index)
			d.HasSuper = true
		}
		descs[i] = d
	}
	return descs, nil
}

func flattenGCTypes(types []wasm.RecType) []wasm.SubType {
	var flat []wasm.SubType
	for _, rt := range types {
		flat = append(flat, rt.SubTypes...)
	}
	return flat
}

func lowerGCStorage(st wasm.StorageType, typeCount int) (gc.StorageKind, error) {
	if st.Packed {
		switch st.Pack {
		case wasm.PackI8:
			return gc.StorageI8, nil
		case wasm.PackI16:
			return gc.StorageI16, nil
		default:
			return 0, fmt.Errorf("unsupported packed storage %d", st.Pack)
		}
	}
	return lowerGCValType(st.Val, typeCount)
}

func lowerGCValType(v wasm.ValType, typeCount int) (gc.StorageKind, error) {
	switch v.Kind {
	case wasm.ValNum:
		switch v.Num {
		case wasm.NumI32:
			return gc.StorageI32, nil
		case wasm.NumI64:
			return gc.StorageI64, nil
		case wasm.NumF32:
			return gc.StorageF32, nil
		case wasm.NumF64:
			return gc.StorageF64, nil
		default:
			return 0, fmt.Errorf("unsupported numeric storage %d", v.Num)
		}
	case wasm.ValVec:
		return 0, fmt.Errorf("unsupported v128 storage")
	case wasm.ValRef:
		if v.Ref.Heap.Kind == wasm.HeapTypeIndex && (v.Ref.Heap.Type.Rec || v.Ref.Heap.Type.Index >= uint32(typeCount)) {
			return 0, fmt.Errorf("invalid referenced type index %d", v.Ref.Heap.Type.Index)
		}
		if v.Ref.Nullable {
			return gc.StorageRefNull, nil
		}
		// All Wasm ref fields use one compact Ref slot. Even i31-only refs are
		// safe to scan because the collector ignores i31 immediates and nulls;
		// eq/any/concrete refs need scanning because they may contain heap refs.
		return gc.StorageRef, nil
	case wasm.ValBot:
		return 0, fmt.Errorf("unsupported bottom storage")
	default:
		return 0, fmt.Errorf("unsupported value kind %d", v.Kind)
	}
}
