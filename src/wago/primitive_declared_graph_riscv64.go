//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The caller checks descriptor grammar first. This conversion preserves every
// recursive member and field, but permits only primitive executable signatures.
func rv64PrimitiveDeclaredGraph(c *Compiled) (*wasm.Module, error) {
	return rv64DeclaredGraph(c, false)
}

// Collector callers must hold the complete rooted execution proof before using
// this structural graph for a native target key. It grants no carrier authority.
func rv64DeclaredGraph(c *Compiled, collector bool) (*wasm.Module, error) {
	groups := int(c.Types[len(c.Types)-1].RecGroup) + 1
	starts := make([]uint32, groups)
	module := &wasm.Module{Types: make([]wasm.RecType, groups)}
	remaining := railshot.MaxPublicCodeBytes / 8
	for index, typ := range c.Types {
		cost := 1 + len(typ.Params) + len(typ.Results) + len(typ.Fields) + len(typ.Supers)
		if typ.HasDescribes || typ.HasDescriptor || cost > remaining {
			return nil, fmt.Errorf("RV64 function declaration graph exceeds profile")
		}
		remaining -= cost
		if index == 0 || typ.RecGroup != c.Types[index-1].RecGroup {
			starts[typ.RecGroup] = uint32(index)
		}
	}
	for group, start := range starts {
		end := uint32(len(c.Types))
		if group+1 < groups {
			end = starts[group+1]
		}
		module.Types[group].SubTypes = make([]wasm.SubType, end-start)
	}
	indexType := func(owner int, index uint32) wasm.TypeIdx {
		result := wasm.TypeIdx{Index: index}
		if c.Types[index].RecGroup == c.Types[owner].RecGroup {
			result.Index -= starts[c.Types[owner].RecGroup]
			result.Rec = true
		}
		return result
	}
	valueType := func(owner int, value ValueTypeDescriptor) (wasm.ValType, error) {
		switch value.Kind {
		case ValueTypeI32:
			return wasm.I32, nil
		case ValueTypeI64:
			return wasm.I64, nil
		case ValueTypeF32:
			return wasm.F32, nil
		case ValueTypeF64:
			return wasm.F64, nil
		case ValueTypeV128:
			return wasm.V128, nil
		}
		if c.Types[owner].Kind == CompositeTypeFunction && !rv64PrimitiveReferenceValue(c, value) && !(collector && rv64CollectorTailDeclaredCarrier(c, value)) {
			return wasm.ValType{}, fmt.Errorf("RV64 function graph reference carrier")
		}
		if value.Ref.Heap.Defined && value.Ref.Heap.Abstract != 0 || !value.Ref.Heap.Defined && value.Ref.Heap.TypeIndex != 0 {
			return wasm.ValType{}, fmt.Errorf("RV64 function graph reference metadata")
		}
		var heap wasm.HeapType
		if value.Ref.Heap.Defined {
			heap = wasm.IndexedHeap(indexType(owner, value.Ref.Heap.TypeIndex))
		} else {
			abstract := [...]wasm.AbsHeapType{wasm.HeapString, wasm.HeapExn, wasm.HeapArray, wasm.HeapStruct, wasm.HeapI31, wasm.HeapEq, wasm.HeapAny, wasm.HeapExtern, wasm.HeapFunc, wasm.HeapNone, wasm.HeapNoExtern, wasm.HeapNoFunc, wasm.HeapNoExn}
			if int(value.Ref.Heap.Abstract) >= len(abstract) {
				return wasm.ValType{}, fmt.Errorf("RV64 function graph abstract heap")
			}
			heap = wasm.AbsHeap(abstract[value.Ref.Heap.Abstract])
		}
		return wasm.RefVal(wasm.Ref(value.Ref.Nullable, heap, value.Ref.Exact)), nil
	}
	fieldType := func(owner int, field FieldTypeDescriptor) (wasm.FieldType, error) {
		var storage wasm.StorageType
		if field.Storage.Packed {
			packed := wasm.PackI8
			if field.Storage.PackedType == PackedTypeI16 {
				packed = wasm.PackI16
			}
			storage = wasm.StoragePacked(packed)
		} else {
			value, err := valueType(owner, field.Storage.Value)
			if err != nil {
				return wasm.FieldType{}, err
			}
			storage = wasm.StorageVal(value)
		}
		mutability := wasm.Const
		if field.Mutable {
			mutability = wasm.Var
		}
		return wasm.NewFieldType(storage, mutability), nil
	}
	for index, typ := range c.Types {
		sub := &module.Types[typ.RecGroup].SubTypes[uint32(index)-starts[typ.RecGroup]]
		sub.Final, sub.HasPrefix = typ.Final, !typ.Final || len(typ.Supers) != 0
		for _, super := range typ.Supers {
			sub.Supers = append(sub.Supers, indexType(index, super))
		}
		switch typ.Kind {
		case CompositeTypeFunction:
			sub.Comp.Kind = wasm.CompFunc
			sub.Comp.Params = make([]wasm.ValType, len(typ.Params))
			sub.Comp.Results = make([]wasm.ValType, len(typ.Results))
			for side, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
				dst := sub.Comp.Params
				if side == 1 {
					dst = sub.Comp.Results
				}
				for position, value := range values {
					converted, err := valueType(index, value)
					if err != nil {
						return nil, err
					}
					dst[position] = converted
				}
			}
		case CompositeTypeStruct:
			sub.Comp.Kind = wasm.CompStruct
			sub.Comp.Fields = make([]wasm.FieldType, len(typ.Fields))
			for position, field := range typ.Fields {
				converted, err := fieldType(index, field)
				if err != nil {
					return nil, err
				}
				sub.Comp.Fields[position] = converted
			}
		case CompositeTypeArray:
			sub.Comp.Kind = wasm.CompArray
			converted, err := fieldType(index, typ.Array)
			if err != nil {
				return nil, err
			}
			sub.Comp.Array = converted
		}
	}
	return module, nil
}
