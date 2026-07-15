package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// ValueTypeKind classifies a self-contained WebAssembly value-type descriptor.
// Reference details live in ValueTypeDescriptor.Ref instead of being collapsed
// to the legacy ValFuncRef/ValExternRef ABI categories.
type ValueTypeKind uint8

const (
	ValueTypeI32 ValueTypeKind = iota
	ValueTypeI64
	ValueTypeF32
	ValueTypeF64
	ValueTypeV128
	ValueTypeReference
)

// AbstractHeapType is the public representation of a WebAssembly abstract heap
// type. Defined heap types use HeapTypeDescriptor.TypeIndex instead.
type AbstractHeapType uint8

const (
	AbstractHeapString AbstractHeapType = iota
	AbstractHeapExn
	AbstractHeapArray
	AbstractHeapStruct
	AbstractHeapI31
	AbstractHeapEq
	AbstractHeapAny
	AbstractHeapExtern
	AbstractHeapFunc
	AbstractHeapNone
	AbstractHeapNoExtern
	AbstractHeapNoFunc
	AbstractHeapNoExn
)

// HeapTypeDescriptor identifies either an abstract heap type or one flattened
// entry in the containing module's DefinedTypeDescriptor slice. Defined is
// explicit so type index zero is never confused with an abstract heap type.
type HeapTypeDescriptor struct {
	Defined   bool
	Abstract  AbstractHeapType
	TypeIndex uint32
}

// ReferenceTypeDescriptor preserves the nullability, exactness, and heap
// identity of a WebAssembly reference type.
type ReferenceTypeDescriptor struct {
	Nullable bool
	Exact    bool
	Heap     HeapTypeDescriptor
}

// ValueTypeDescriptor is wago's public structural value-type representation.
// It is independent of internal decoder packages and preserves indexed
// reference identity. Type indexes resolve against the module metadata's Types
// slice, whose recursive graph is serialized with the descriptor.
type ValueTypeDescriptor struct {
	Kind ValueTypeKind
	Ref  ReferenceTypeDescriptor
}

// ABIType reports the existing public ABI category for values that the current
// runtime can carry. types is the containing module's flattened type graph and
// is consulted for indexed references. GC-managed references deliberately
// return false until the public value/store boundary supports them.
func (t ValueTypeDescriptor) ABIType(types []DefinedTypeDescriptor) (ValType, bool) {
	switch t.Kind {
	case ValueTypeI32:
		return ValI32, true
	case ValueTypeI64:
		return ValI64, true
	case ValueTypeF32:
		return ValF32, true
	case ValueTypeF64:
		return ValF64, true
	case ValueTypeV128:
		return ValV128, true
	case ValueTypeReference:
		if t.Ref.Heap.Defined {
			if int(t.Ref.Heap.TypeIndex) >= len(types) || types[t.Ref.Heap.TypeIndex].Kind != CompositeTypeFunction {
				return 0, false
			}
			return ValFuncRef, true
		}
		switch t.Ref.Heap.Abstract {
		case AbstractHeapFunc, AbstractHeapNoFunc:
			return ValFuncRef, true
		case AbstractHeapExtern, AbstractHeapNoExtern:
			return ValExternRef, true
		default:
			return 0, false
		}
	default:
		return 0, false
	}
}

// CompositeTypeKind identifies a function, struct, or array definition.
type CompositeTypeKind uint8

const (
	CompositeTypeFunction CompositeTypeKind = iota
	CompositeTypeStruct
	CompositeTypeArray
)

// PackedType identifies a packed GC field representation.
type PackedType uint8

const (
	PackedTypeI8 PackedType = iota
	PackedTypeI16
)

// StorageTypeDescriptor is one unpacked value type or packed integer field.
type StorageTypeDescriptor struct {
	Packed     bool
	PackedType PackedType
	Value      ValueTypeDescriptor
}

// FieldTypeDescriptor is one struct field or the element field of an array.
type FieldTypeDescriptor struct {
	Storage StorageTypeDescriptor
	Mutable bool
}

// DefinedTypeDescriptor is one flattened entry from a WebAssembly recursive
// type section. RecGroup preserves group boundaries; every type reference is an
// absolute flattened TypeIndex, including references originally encoded relative
// to the current recursive group.
type DefinedTypeDescriptor struct {
	RecGroup uint32
	Final    bool
	Supers   []uint32

	HasDescribes  bool
	Describes     uint32
	HasDescriptor bool
	Descriptor    uint32

	Kind    CompositeTypeKind
	Params  []ValueTypeDescriptor
	Results []ValueTypeDescriptor
	Fields  []FieldTypeDescriptor
	Array   FieldTypeDescriptor
}

type wasmTypeDescriptorConverter struct {
	m       *wasm.Module
	groupAt []uint32
}

func typeDescriptorsFromWasm(m *wasm.Module) ([]DefinedTypeDescriptor, error) {
	if m == nil {
		return nil, fmt.Errorf("nil wasm module")
	}
	c := wasmTypeDescriptorConverter{m: m, groupAt: make([]uint32, len(m.Types)+1)}
	for i := range m.Types {
		c.groupAt[i+1] = c.groupAt[i] + uint32(len(m.Types[i].SubTypes))
	}
	out := make([]DefinedTypeDescriptor, 0, c.groupAt[len(m.Types)])
	for gi := range m.Types {
		for si := range m.Types[gi].SubTypes {
			d, err := c.definedType(&m.Types[gi].SubTypes[si], gi)
			if err != nil {
				return nil, fmt.Errorf("type %d: %w", len(out), err)
			}
			out = append(out, d)
		}
	}
	return out, nil
}

func valueTypeDescriptorFromWasm(t wasm.ValType) (ValueTypeDescriptor, error) {
	return (wasmTypeDescriptorConverter{}).valueType(t, -1)
}

func valueTypeDescriptorsFromWasm(ts []wasm.ValType) ([]ValueTypeDescriptor, error) {
	out := make([]ValueTypeDescriptor, len(ts))
	for i := range ts {
		var err error
		out[i], err = valueTypeDescriptorFromWasm(ts[i])
		if err != nil {
			return nil, fmt.Errorf("value %d: %w", i, err)
		}
	}
	return out, nil
}

func (c wasmTypeDescriptorConverter) definedType(st *wasm.SubType, group int) (DefinedTypeDescriptor, error) {
	d := DefinedTypeDescriptor{RecGroup: uint32(group), Final: st.Final}
	for _, idx := range st.Supers {
		x, err := c.typeIndex(idx, group)
		if err != nil {
			return d, fmt.Errorf("supertype: %w", err)
		}
		d.Supers = append(d.Supers, x)
	}
	if st.Metadata.Describes != nil {
		x, err := c.typeIndex(*st.Metadata.Describes, group)
		if err != nil {
			return d, fmt.Errorf("describes: %w", err)
		}
		d.HasDescribes, d.Describes = true, x
	}
	if st.Metadata.Descriptor != nil {
		x, err := c.typeIndex(*st.Metadata.Descriptor, group)
		if err != nil {
			return d, fmt.Errorf("descriptor: %w", err)
		}
		d.HasDescriptor, d.Descriptor = true, x
	}
	switch st.Comp.Kind {
	case wasm.CompFunc:
		d.Kind = CompositeTypeFunction
		var err error
		d.Params, err = c.valueTypes(st.Comp.Params, group)
		if err != nil {
			return d, fmt.Errorf("params: %w", err)
		}
		d.Results, err = c.valueTypes(st.Comp.Results, group)
		if err != nil {
			return d, fmt.Errorf("results: %w", err)
		}
	case wasm.CompStruct:
		d.Kind = CompositeTypeStruct
		d.Fields = make([]FieldTypeDescriptor, len(st.Comp.Fields))
		for i := range st.Comp.Fields {
			f, err := c.fieldType(st.Comp.Fields[i], group)
			if err != nil {
				return d, fmt.Errorf("field %d: %w", i, err)
			}
			d.Fields[i] = f
		}
	case wasm.CompArray:
		d.Kind = CompositeTypeArray
		f, err := c.fieldType(st.Comp.Array, group)
		if err != nil {
			return d, fmt.Errorf("array field: %w", err)
		}
		d.Array = f
	default:
		return d, fmt.Errorf("unknown composite type kind %d", st.Comp.Kind)
	}
	return d, nil
}

func (c wasmTypeDescriptorConverter) fieldType(f wasm.FieldType, group int) (FieldTypeDescriptor, error) {
	out := FieldTypeDescriptor{Mutable: f.Mut == wasm.Var}
	if f.Storage.Packed {
		out.Storage.Packed = true
		switch f.Storage.Pack {
		case wasm.PackI8:
			out.Storage.PackedType = PackedTypeI8
		case wasm.PackI16:
			out.Storage.PackedType = PackedTypeI16
		default:
			return out, fmt.Errorf("unknown packed type 0x%x", byte(f.Storage.Pack))
		}
		return out, nil
	}
	v, err := c.valueType(f.Storage.Val, group)
	if err != nil {
		return out, err
	}
	out.Storage.Value = v
	return out, nil
}

func (c wasmTypeDescriptorConverter) valueTypes(ts []wasm.ValType, group int) ([]ValueTypeDescriptor, error) {
	out := make([]ValueTypeDescriptor, len(ts))
	for i := range ts {
		v, err := c.valueType(ts[i], group)
		if err != nil {
			return nil, fmt.Errorf("value %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

func (c wasmTypeDescriptorConverter) valueType(t wasm.ValType, group int) (ValueTypeDescriptor, error) {
	switch t.Kind {
	case wasm.ValNum:
		switch t.Num {
		case wasm.NumI32:
			return ValueTypeDescriptor{Kind: ValueTypeI32}, nil
		case wasm.NumI64:
			return ValueTypeDescriptor{Kind: ValueTypeI64}, nil
		case wasm.NumF32:
			return ValueTypeDescriptor{Kind: ValueTypeF32}, nil
		case wasm.NumF64:
			return ValueTypeDescriptor{Kind: ValueTypeF64}, nil
		default:
			return ValueTypeDescriptor{}, fmt.Errorf("unknown numeric type 0x%x", byte(t.Num))
		}
	case wasm.ValVec:
		return ValueTypeDescriptor{Kind: ValueTypeV128}, nil
	case wasm.ValRef:
		r, err := c.refType(t.Ref, group)
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: r}, err
	default:
		return ValueTypeDescriptor{}, fmt.Errorf("unsupported value type kind %d", t.Kind)
	}
}

func (c wasmTypeDescriptorConverter) refType(t wasm.RefType, group int) (ReferenceTypeDescriptor, error) {
	out := ReferenceTypeDescriptor{Nullable: t.Nullable, Exact: t.Exact}
	switch t.Heap.Kind {
	case wasm.HeapAbs:
		a, ok := abstractHeapTypeFromWasm(t.Heap.Abs)
		if !ok {
			return out, fmt.Errorf("unknown abstract heap type 0x%x", byte(t.Heap.Abs))
		}
		out.Heap.Abstract = a
	case wasm.HeapTypeIndex:
		x, err := c.typeIndex(t.Heap.Type, group)
		if err != nil {
			return out, err
		}
		out.Heap.Defined, out.Heap.TypeIndex = true, x
	case wasm.HeapDefType:
		if t.Heap.Def == nil || int(t.Heap.Def.GroupIndex) >= len(c.groupAt)-1 || t.Heap.Def.Index >= uint32(len(c.m.Types[t.Heap.Def.GroupIndex].SubTypes)) {
			return out, fmt.Errorf("unknown defined heap type")
		}
		out.Heap.Defined = true
		out.Heap.TypeIndex = c.groupAt[t.Heap.Def.GroupIndex] + t.Heap.Def.Index
	default:
		return out, fmt.Errorf("unknown heap type kind %d", t.Heap.Kind)
	}
	return out, nil
}

func (c wasmTypeDescriptorConverter) typeIndex(idx wasm.TypeIdx, group int) (uint32, error) {
	if !idx.Rec {
		if len(c.groupAt) != 0 && idx.Index >= c.groupAt[len(c.groupAt)-1] {
			return 0, fmt.Errorf("type index %d out of range", idx.Index)
		}
		return idx.Index, nil
	}
	if group < 0 || group >= len(c.groupAt)-1 || idx.Index >= uint32(len(c.m.Types[group].SubTypes)) {
		return 0, fmt.Errorf("recursive type index %d out of range", idx.Index)
	}
	return c.groupAt[group] + idx.Index, nil
}

func abstractHeapTypeFromWasm(t wasm.AbsHeapType) (AbstractHeapType, bool) {
	switch t {
	case wasm.HeapString:
		return AbstractHeapString, true
	case wasm.HeapExn:
		return AbstractHeapExn, true
	case wasm.HeapArray:
		return AbstractHeapArray, true
	case wasm.HeapStruct:
		return AbstractHeapStruct, true
	case wasm.HeapI31:
		return AbstractHeapI31, true
	case wasm.HeapEq:
		return AbstractHeapEq, true
	case wasm.HeapAny:
		return AbstractHeapAny, true
	case wasm.HeapExtern:
		return AbstractHeapExtern, true
	case wasm.HeapFunc:
		return AbstractHeapFunc, true
	case wasm.HeapNone:
		return AbstractHeapNone, true
	case wasm.HeapNoExtern:
		return AbstractHeapNoExtern, true
	case wasm.HeapNoFunc:
		return AbstractHeapNoFunc, true
	case wasm.HeapNoExn:
		return AbstractHeapNoExn, true
	default:
		return 0, false
	}
}
