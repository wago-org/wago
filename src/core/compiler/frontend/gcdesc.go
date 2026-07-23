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
	for i, ft := range flat {
		st := ft.SubType
		id := gc.TypeID(i)
		resolver := gcTypeResolver{total: len(flat), recBase: ft.RecBase, recLen: ft.RecLen, flat: flat}
		var d gc.TypeDesc
		var err error
		switch st.Comp.Kind {
		case wasm.CompFunc:
			d = gc.TypeDesc{ID: id, Kind: gc.KindFunc, Final: st.Final}
		case wasm.CompStruct:
			fields := make([]gc.StorageKind, len(st.Comp.Fields))
			for j, f := range st.Comp.Fields {
				fields[j], err = lowerGCStorage(f.Storage, resolver)
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
			elem, err := lowerGCStorage(st.Comp.Array.Storage, resolver)
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
			super, err := resolver.resolve(st.Supers[0])
			if err != nil {
				return nil, fmt.Errorf("frontend: type %d has invalid super type index %d", i, st.Supers[0].Index)
			}
			d.Super = gc.TypeID(super)
			d.HasSuper = true
		}
		descs[i] = d
	}
	if err := gc.ValidateTypeDescs(descs); err != nil {
		return nil, fmt.Errorf("frontend: lowered GC type descriptors invalid: %w", err)
	}
	return descs, nil
}

type flattenedGCType struct {
	wasm.SubType
	RecBase int
	RecLen  int
}

type gcTypeResolver struct {
	total   int
	recBase int
	recLen  int
	flat    []flattenedGCType
}

func (r gcTypeResolver) resolve(idx wasm.TypeIdx) (uint32, error) {
	if idx.Rec {
		if idx.Index >= uint32(r.recLen) {
			return 0, fmt.Errorf("invalid recursive type index %d", idx.Index)
		}
		return uint32(r.recBase) + idx.Index, nil
	}
	if idx.Index >= uint32(r.total) {
		return 0, fmt.Errorf("invalid type index %d", idx.Index)
	}
	return idx.Index, nil
}

func flattenGCTypes(types []wasm.RecType) []flattenedGCType {
	var flat []flattenedGCType
	for _, rt := range types {
		base := len(flat)
		for _, st := range rt.SubTypes {
			flat = append(flat, flattenedGCType{SubType: st, RecBase: base, RecLen: len(rt.SubTypes)})
		}
	}
	return flat
}

func lowerGCStorage(st wasm.StorageType, resolver gcTypeResolver) (gc.StorageKind, error) {
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
	return lowerGCValType(st.Val, resolver)
}

func lowerGCValType(v wasm.ValType, resolver gcTypeResolver) (gc.StorageKind, error) {
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
		opaque := gc.StorageKind(0)
		if v.Ref.Heap.Kind == wasm.HeapTypeIndex {
			idx, err := resolver.resolve(v.Ref.Heap.Type)
			if err != nil {
				return 0, fmt.Errorf("invalid referenced type index %d", v.Ref.Heap.Type.Index)
			}
			if int(idx) < len(resolver.flat) && resolver.flat[idx].Comp.Kind == wasm.CompFunc {
				opaque = gc.StorageFuncRef
			}
		} else {
			switch v.Ref.Heap.Abs {
			case wasm.HeapFunc, wasm.HeapNoFunc:
				opaque = gc.StorageFuncRef
			case wasm.HeapExtern, wasm.HeapNoExtern:
				opaque = gc.StorageExternRef
			}
		}
		if opaque != 0 {
			if v.Ref.Nullable {
				if opaque == gc.StorageFuncRef {
					return gc.StorageFuncRefNull, nil
				}
				return gc.StorageExternRefNull, nil
			}
			return opaque, nil
		}
		if v.Ref.Nullable {
			return gc.StorageRefNull, nil
		}
		// GC-category references use compact collector handles. i31 immediates and
		// null are ignored by scanning; struct/array/eq/any refs are traced.
		return gc.StorageRef, nil
	case wasm.ValBot:
		return 0, fmt.Errorf("unsupported bottom storage")
	default:
		return 0, fmt.Errorf("unsupported value kind %d", v.Kind)
	}
}
