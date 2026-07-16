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

// ABIType reports the one-slot public/product ABI category for values that the
// current runtime can describe. types is the containing module's flattened type
// graph and is consulted for indexed references. Struct/array references use
// ValAnyRef metadata slots. Non-null ingress remains fail-closed; the exact
// staged basic-struct result may egress only through a bounded store-owned token.
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
			if int(t.Ref.Heap.TypeIndex) >= len(types) {
				return 0, false
			}
			switch types[t.Ref.Heap.TypeIndex].Kind {
			case CompositeTypeFunction:
				return ValFuncRef, true
			case CompositeTypeStruct, CompositeTypeArray:
				return ValAnyRef, true
			default:
				return 0, false
			}
		}
		switch t.Ref.Heap.Abstract {
		case AbstractHeapFunc, AbstractHeapNoFunc:
			return ValFuncRef, true
		case AbstractHeapExtern, AbstractHeapNoExtern:
			return ValExternRef, true
		case AbstractHeapExn, AbstractHeapNoExn:
			return ValExnRef, true
		case AbstractHeapAny, AbstractHeapEq, AbstractHeapNone, AbstractHeapStruct, AbstractHeapArray:
			return ValAnyRef, true
		case AbstractHeapI31:
			return ValI31Ref, true
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

func newWasmTypeDescriptorConverter(m *wasm.Module) wasmTypeDescriptorConverter {
	c := wasmTypeDescriptorConverter{m: m}
	if m != nil {
		c.groupAt = make([]uint32, len(m.Types)+1)
		for i := range m.Types {
			c.groupAt[i+1] = c.groupAt[i] + uint32(len(m.Types[i].SubTypes))
		}
	}
	return c
}

func (c wasmTypeDescriptorConverter) abiType(t wasm.ValType, types []DefinedTypeDescriptor) (ValType, error) {
	exact, err := c.valueType(t, -1)
	if err != nil {
		return 0, err
	}
	abi, ok := exact.ABIType(types)
	if !ok {
		return 0, fmt.Errorf("structural type is outside the current public ABI")
	}
	return abi, nil
}

func (c wasmTypeDescriptorConverter) abiTypes(ts []wasm.ValType, types []DefinedTypeDescriptor) ([]ValType, error) {
	exact, err := c.valueTypes(ts, -1)
	if err != nil {
		return nil, err
	}
	out := make([]ValType, len(exact))
	for i := range exact {
		abi, ok := exact[i].ABIType(types)
		if !ok {
			return nil, fmt.Errorf("value %d: structural type is outside the current public ABI", i)
		}
		out[i] = abi
	}
	return out, nil
}

func typeDescriptorsFromWasm(m *wasm.Module) ([]DefinedTypeDescriptor, error) {
	if m == nil {
		return nil, fmt.Errorf("nil wasm module")
	}
	c := newWasmTypeDescriptorConverter(m)
	out := make([]DefinedTypeDescriptor, 0, c.groupAt[len(m.Types)])
	descriptorGroup := uint32(0)
	for gi := range m.Types {
		if len(m.Types[gi].SubTypes) == 0 {
			continue
		}
		for si := range m.Types[gi].SubTypes {
			d, err := c.definedType(&m.Types[gi].SubTypes[si], gi, descriptorGroup)
			if err != nil {
				return nil, fmt.Errorf("type %d: %w", len(out), err)
			}
			out = append(out, d)
		}
		descriptorGroup++
	}
	return out, nil
}

func valueTypeDescriptorFromWasm(t wasm.ValType) (ValueTypeDescriptor, error) {
	return (wasmTypeDescriptorConverter{}).valueType(t, -1)
}

func valueTypeDescriptorsFromWasm(ts []wasm.ValType) ([]ValueTypeDescriptor, error) {
	return valueTypeDescriptorsInModule(nil, ts)
}

func valueTypeDescriptorInModule(m *wasm.Module, t wasm.ValType) (ValueTypeDescriptor, error) {
	v, err := valueTypeDescriptorsInModule(m, []wasm.ValType{t})
	if err != nil {
		return ValueTypeDescriptor{}, err
	}
	return v[0], nil
}

func valueTypeDescriptorsInModule(m *wasm.Module, ts []wasm.ValType) ([]ValueTypeDescriptor, error) {
	return newWasmTypeDescriptorConverter(m).valueTypes(ts, -1)
}

func valTypeFromWasmInModule(m *wasm.Module, t wasm.ValType, types []DefinedTypeDescriptor) (ValType, error) {
	return newWasmTypeDescriptorConverter(m).abiType(t, types)
}

func valTypesFromWasmInModule(m *wasm.Module, ts []wasm.ValType, types []DefinedTypeDescriptor) ([]ValType, error) {
	return newWasmTypeDescriptorConverter(m).abiTypes(ts, types)
}

func valueTypeDescriptorFromValType(t ValType) (ValueTypeDescriptor, bool) {
	switch t {
	case ValI32:
		return ValueTypeDescriptor{Kind: ValueTypeI32}, true
	case ValI64:
		return ValueTypeDescriptor{Kind: ValueTypeI64}, true
	case ValF32:
		return ValueTypeDescriptor{Kind: ValueTypeF32}, true
	case ValF64:
		return ValueTypeDescriptor{Kind: ValueTypeF64}, true
	case ValV128:
		return ValueTypeDescriptor{Kind: ValueTypeV128}, true
	case ValFuncRef:
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}, true
	case ValExternRef:
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}, true
	case ValExnRef:
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExn}}}, true
	case ValAnyRef:
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}, true
	case ValI31Ref:
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapI31}}}, true
	default:
		return ValueTypeDescriptor{}, false
	}
}

func valueTypeDescriptorsFromValTypes(ts []ValType) ([]ValueTypeDescriptor, error) {
	out := make([]ValueTypeDescriptor, len(ts))
	for i, t := range ts {
		v, ok := valueTypeDescriptorFromValType(t)
		if !ok {
			return nil, fmt.Errorf("value %d: unsupported ABI value type %s", i, t)
		}
		out[i] = v
	}
	return out, nil
}

func valTypesFromDescriptors(ts []ValueTypeDescriptor, types []DefinedTypeDescriptor) ([]ValType, error) {
	out := make([]ValType, len(ts))
	for i, t := range ts {
		v, ok := t.ABIType(types)
		if !ok {
			return nil, fmt.Errorf("value %d: structural type is outside the current public ABI", i)
		}
		out[i] = v
	}
	return out, nil
}

func equalValTypes(a, b []ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func exactValueType(legacy ValType, has bool, index uint32, pool []ValueTypeDescriptor, types []DefinedTypeDescriptor) (ValueTypeDescriptor, error) {
	if !has {
		v, ok := valueTypeDescriptorFromValType(legacy)
		if !ok {
			return ValueTypeDescriptor{}, fmt.Errorf("unsupported ABI value type %s", legacy)
		}
		return v, nil
	}
	if int(index) >= len(pool) {
		return ValueTypeDescriptor{}, fmt.Errorf("value type index %d out of range", index)
	}
	v := pool[index]
	abi, ok := v.ABIType(types)
	if !ok || abi != legacy {
		return ValueTypeDescriptor{}, fmt.Errorf("value type index %d does not match ABI type %s", index, legacy)
	}
	return v, nil
}

func internValueType(pool *[]ValueTypeDescriptor, t ValueTypeDescriptor) uint32 {
	for i := range *pool {
		if (*pool)[i] == t {
			return uint32(i)
		}
	}
	*pool = append(*pool, t)
	return uint32(len(*pool) - 1)
}

// valueTypeSubtype compares exact descriptors from independent compiled modules.
// Defined indexes are interpreted against their containing type graphs, so raw
// index equality is never used as cross-module identity. The coinductive pair
// walk is bounded by the reachable product of the two finite type graphs.
func valueTypeSubtype(actual ValueTypeDescriptor, actualTypes []DefinedTypeDescriptor, required ValueTypeDescriptor, requiredTypes []DefinedTypeDescriptor) bool {
	if actual.Kind != required.Kind {
		return false
	}
	if actual.Kind != ValueTypeReference {
		return true
	}
	return referenceTypeSubtype(actual.Ref, actualTypes, required.Ref, requiredTypes)
}

func valueTypeEquivalent(a ValueTypeDescriptor, aTypes []DefinedTypeDescriptor, b ValueTypeDescriptor, bTypes []DefinedTypeDescriptor) bool {
	return valueTypeSubtype(a, aTypes, b, bTypes) && valueTypeSubtype(b, bTypes, a, aTypes)
}

func referenceTypeSubtype(actual ReferenceTypeDescriptor, actualTypes []DefinedTypeDescriptor, required ReferenceTypeDescriptor, requiredTypes []DefinedTypeDescriptor) bool {
	if actual.Nullable && !required.Nullable {
		return false
	}
	if heapTypeEquivalent(actual.Heap, actualTypes, required.Heap, requiredTypes) {
		return !required.Exact || actual.Exact
	}
	if required.Exact {
		return false
	}
	if actual.Heap.Defined {
		if required.Heap.Defined {
			return definedTypeHasSuper(actual.Heap.TypeIndex, actualTypes, required.Heap.TypeIndex, requiredTypes)
		}
		if int(actual.Heap.TypeIndex) >= len(actualTypes) {
			return false
		}
		var family AbstractHeapType
		switch actualTypes[actual.Heap.TypeIndex].Kind {
		case CompositeTypeFunction:
			family = AbstractHeapFunc
		case CompositeTypeStruct:
			family = AbstractHeapStruct
		case CompositeTypeArray:
			family = AbstractHeapArray
		default:
			return false
		}
		return abstractHeapSubtype(family, required.Heap.Abstract)
	}
	if required.Heap.Defined {
		if int(required.Heap.TypeIndex) >= len(requiredTypes) {
			return false
		}
		switch actual.Heap.Abstract {
		case AbstractHeapNoFunc:
			return requiredTypes[required.Heap.TypeIndex].Kind == CompositeTypeFunction
		case AbstractHeapNone:
			kind := requiredTypes[required.Heap.TypeIndex].Kind
			return kind == CompositeTypeStruct || kind == CompositeTypeArray
		default:
			return false
		}
	}
	return abstractHeapSubtype(actual.Heap.Abstract, required.Heap.Abstract)
}

func abstractHeapSubtype(actual, required AbstractHeapType) bool {
	if actual == required {
		return true
	}
	switch actual {
	case AbstractHeapNoFunc:
		return required == AbstractHeapFunc
	case AbstractHeapNoExtern:
		return required == AbstractHeapExtern
	case AbstractHeapNoExn:
		return required == AbstractHeapExn
	case AbstractHeapNone:
		return required == AbstractHeapAny || required == AbstractHeapEq || required == AbstractHeapStruct || required == AbstractHeapArray || required == AbstractHeapI31
	case AbstractHeapI31, AbstractHeapStruct, AbstractHeapArray:
		return required == AbstractHeapEq || required == AbstractHeapAny
	case AbstractHeapEq:
		return required == AbstractHeapAny
	}
	return false
}

func definedTypeHasSuper(actual uint32, actualTypes []DefinedTypeDescriptor, required uint32, requiredTypes []DefinedTypeDescriptor) bool {
	seen := make(map[uint32]bool)
	var visit func(uint32) bool
	visit = func(index uint32) bool {
		if int(index) >= len(actualTypes) || seen[index] {
			return false
		}
		seen[index] = true
		if definedTypeEquivalent(index, actualTypes, required, requiredTypes) {
			return true
		}
		for _, super := range actualTypes[index].Supers {
			if visit(super) {
				return true
			}
		}
		return false
	}
	return visit(actual)
}

func heapTypeEquivalent(a HeapTypeDescriptor, aTypes []DefinedTypeDescriptor, b HeapTypeDescriptor, bTypes []DefinedTypeDescriptor) bool {
	if a.Defined != b.Defined {
		return false
	}
	if !a.Defined {
		return a.Abstract == b.Abstract
	}
	return definedTypeEquivalent(a.TypeIndex, aTypes, b.TypeIndex, bTypes)
}

func definedTypeEquivalent(a uint32, aTypes []DefinedTypeDescriptor, b uint32, bTypes []DefinedTypeDescriptor) bool {
	type pair struct{ a, b uint32 }
	state := make(map[pair]uint8)
	groupBounds := func(types []DefinedTypeDescriptor, index uint32) (start, end uint32, ok bool) {
		if int(index) >= len(types) {
			return 0, 0, false
		}
		group := types[index].RecGroup
		start, end = index, index+1
		for start > 0 && types[start-1].RecGroup == group {
			start--
		}
		for int(end) < len(types) && types[end].RecGroup == group {
			end++
		}
		return start, end, true
	}
	var eqType func(uint32, uint32) bool
	var eqTypeBody func(uint32, uint32) bool
	eqTypeRef := func(ownerX, x, ownerY, y uint32) bool {
		if int(ownerX) >= len(aTypes) || int(x) >= len(aTypes) || int(ownerY) >= len(bTypes) || int(y) >= len(bTypes) {
			return false
		}
		xBound := aTypes[ownerX].RecGroup == aTypes[x].RecGroup
		yBound := bTypes[ownerY].RecGroup == bTypes[y].RecGroup
		return xBound == yBound && eqType(x, y)
	}
	eqHeap := func(ownerX, ownerY uint32, x, y HeapTypeDescriptor) bool {
		if x.Defined != y.Defined {
			return false
		}
		if !x.Defined {
			return x.Abstract == y.Abstract
		}
		return eqTypeRef(ownerX, x.TypeIndex, ownerY, y.TypeIndex)
	}
	eqValue := func(ownerX, ownerY uint32, x, y ValueTypeDescriptor) bool {
		if x.Kind != y.Kind {
			return false
		}
		if x.Kind != ValueTypeReference {
			return true
		}
		return x.Ref.Nullable == y.Ref.Nullable && x.Ref.Exact == y.Ref.Exact && eqHeap(ownerX, ownerY, x.Ref.Heap, y.Ref.Heap)
	}
	eqStorage := func(ownerX, ownerY uint32, x, y StorageTypeDescriptor) bool {
		return x.Packed == y.Packed && x.PackedType == y.PackedType && (x.Packed || eqValue(ownerX, ownerY, x.Value, y.Value))
	}
	eqField := func(ownerX, ownerY uint32, x, y FieldTypeDescriptor) bool {
		return x.Mutable == y.Mutable && eqStorage(ownerX, ownerY, x.Storage, y.Storage)
	}
	eqOptional := func(ownerX uint32, xHas bool, x uint32, ownerY uint32, yHas bool, y uint32) bool {
		return xHas == yHas && (!xHas || eqTypeRef(ownerX, x, ownerY, y))
	}
	eqTypeBody = func(x, y uint32) bool {
		xd, yd := aTypes[x], bTypes[y]
		ok := xd.Final == yd.Final && xd.Kind == yd.Kind && len(xd.Supers) == len(yd.Supers) &&
			eqOptional(x, xd.HasDescribes, xd.Describes, y, yd.HasDescribes, yd.Describes) &&
			eqOptional(x, xd.HasDescriptor, xd.Descriptor, y, yd.HasDescriptor, yd.Descriptor)
		for i := 0; ok && i < len(xd.Supers); i++ {
			ok = eqTypeRef(x, xd.Supers[i], y, yd.Supers[i])
		}
		if !ok {
			return false
		}
		switch xd.Kind {
		case CompositeTypeFunction:
			ok = len(xd.Params) == len(yd.Params) && len(xd.Results) == len(yd.Results)
			for i := 0; ok && i < len(xd.Params); i++ {
				ok = eqValue(x, y, xd.Params[i], yd.Params[i])
			}
			for i := 0; ok && i < len(xd.Results); i++ {
				ok = eqValue(x, y, xd.Results[i], yd.Results[i])
			}
		case CompositeTypeStruct:
			ok = len(xd.Fields) == len(yd.Fields)
			for i := 0; ok && i < len(xd.Fields); i++ {
				ok = eqField(x, y, xd.Fields[i], yd.Fields[i])
			}
		case CompositeTypeArray:
			ok = eqField(x, y, xd.Array, yd.Array)
		default:
			ok = false
		}
		return ok
	}
	eqType = func(x, y uint32) bool {
		xStart, xEnd, xOK := groupBounds(aTypes, x)
		yStart, yEnd, yOK := groupBounds(bTypes, y)
		if !xOK || !yOK || x-xStart != y-yStart || xEnd-xStart != yEnd-yStart {
			return false
		}
		p := pair{xStart, yStart}
		switch state[p] {
		case 1, 2:
			return true
		case 3:
			return false
		}
		state[p] = 1
		ok := true
		for i := uint32(0); ok && i < xEnd-xStart; i++ {
			ok = eqTypeBody(xStart+i, yStart+i)
		}
		if ok {
			state[p] = 2
		} else {
			state[p] = 3
		}
		return ok
	}
	return eqType(a, b)
}

func exactFuncSignatureView(sig FuncSig, types []DefinedTypeDescriptor) (params, results []ValueTypeDescriptor, err error) {
	if !sig.HasTypeIndex {
		return nil, nil, nil
	}
	if int(sig.TypeIndex) >= len(types) {
		return nil, nil, fmt.Errorf("type index %d out of range", sig.TypeIndex)
	}
	d := types[sig.TypeIndex]
	if d.Kind != CompositeTypeFunction {
		return nil, nil, fmt.Errorf("type index %d is not a function", sig.TypeIndex)
	}
	return d.Params, d.Results, nil
}

func exactFuncSignature(sig FuncSig, types []DefinedTypeDescriptor) (params, results []ValueTypeDescriptor, err error) {
	if !sig.HasTypeIndex {
		params, err = valueTypeDescriptorsFromValTypes(sig.Params)
		if err != nil {
			return nil, nil, err
		}
		results, err = valueTypeDescriptorsFromValTypes(sig.Results)
		return params, results, err
	}
	params, results, err = exactFuncSignatureView(sig, types)
	if err != nil {
		return nil, nil, err
	}
	legacyParams, e := valTypesFromDescriptors(params, types)
	if e != nil || !equalValTypes(legacyParams, sig.Params) {
		return nil, nil, fmt.Errorf("structural params do not match ABI params")
	}
	legacyResults, e := valTypesFromDescriptors(results, types)
	if e != nil || !equalValTypes(legacyResults, sig.Results) {
		return nil, nil, fmt.Errorf("structural results do not match ABI results")
	}
	return append([]ValueTypeDescriptor(nil), params...), append([]ValueTypeDescriptor(nil), results...), nil
}

func cloneDefinedTypeDescriptors(in []DefinedTypeDescriptor) []DefinedTypeDescriptor {
	out := make([]DefinedTypeDescriptor, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Supers = append([]uint32(nil), in[i].Supers...)
		out[i].Params = append([]ValueTypeDescriptor(nil), in[i].Params...)
		out[i].Results = append([]ValueTypeDescriptor(nil), in[i].Results...)
		out[i].Fields = append([]FieldTypeDescriptor(nil), in[i].Fields...)
	}
	return out
}

func (c wasmTypeDescriptorConverter) definedType(st *wasm.SubType, sourceGroup int, descriptorGroup uint32) (DefinedTypeDescriptor, error) {
	d := DefinedTypeDescriptor{RecGroup: descriptorGroup, Final: st.Final}
	for _, idx := range st.Supers {
		x, err := c.typeIndex(idx, sourceGroup)
		if err != nil {
			return d, fmt.Errorf("supertype: %w", err)
		}
		d.Supers = append(d.Supers, x)
	}
	if st.Metadata.Describes != nil {
		x, err := c.typeIndex(*st.Metadata.Describes, sourceGroup)
		if err != nil {
			return d, fmt.Errorf("describes: %w", err)
		}
		d.HasDescribes, d.Describes = true, x
	}
	if st.Metadata.Descriptor != nil {
		x, err := c.typeIndex(*st.Metadata.Descriptor, sourceGroup)
		if err != nil {
			return d, fmt.Errorf("descriptor: %w", err)
		}
		d.HasDescriptor, d.Descriptor = true, x
	}
	switch st.Comp.Kind {
	case wasm.CompFunc:
		d.Kind = CompositeTypeFunction
		var err error
		d.Params, err = c.valueTypes(st.Comp.Params, sourceGroup)
		if err != nil {
			return d, fmt.Errorf("params: %w", err)
		}
		d.Results, err = c.valueTypes(st.Comp.Results, sourceGroup)
		if err != nil {
			return d, fmt.Errorf("results: %w", err)
		}
	case wasm.CompStruct:
		d.Kind = CompositeTypeStruct
		d.Fields = make([]FieldTypeDescriptor, len(st.Comp.Fields))
		for i := range st.Comp.Fields {
			f, err := c.fieldType(st.Comp.Fields[i], sourceGroup)
			if err != nil {
				return d, fmt.Errorf("field %d: %w", i, err)
			}
			d.Fields[i] = f
		}
	case wasm.CompArray:
		d.Kind = CompositeTypeArray
		f, err := c.fieldType(st.Comp.Array, sourceGroup)
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
		if c.m == nil || t.Heap.Def == nil || int(t.Heap.Def.GroupIndex) >= len(c.groupAt)-1 || t.Heap.Def.Index >= uint32(len(c.m.Types[t.Heap.Def.GroupIndex].SubTypes)) {
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

func validateValueTypeDescriptor(context string, t ValueTypeDescriptor, types []DefinedTypeDescriptor) error {
	if t.Kind > ValueTypeReference {
		return fmt.Errorf("compiled metadata invalid: %s has value type kind %d", context, t.Kind)
	}
	if t.Kind != ValueTypeReference {
		if t.Ref != (ReferenceTypeDescriptor{}) {
			return fmt.Errorf("compiled metadata invalid: %s non-reference type carries reference metadata", context)
		}
		return nil
	}
	if t.Ref.Heap.Defined {
		if int(t.Ref.Heap.TypeIndex) >= len(types) {
			return fmt.Errorf("compiled metadata invalid: %s type index %d out of range", context, t.Ref.Heap.TypeIndex)
		}
	} else {
		if t.Ref.Heap.Abstract > AbstractHeapNoExn {
			return fmt.Errorf("compiled metadata invalid: %s abstract heap type %d invalid", context, t.Ref.Heap.Abstract)
		}
		if t.Ref.Exact {
			return fmt.Errorf("compiled metadata invalid: %s exact abstract heap type is invalid", context)
		}
	}
	return nil
}

func validateValueTypeDescriptors(types []DefinedTypeDescriptor, values []ValueTypeDescriptor) error {
	for i, t := range values {
		if err := validateValueTypeDescriptor(fmt.Sprintf("value type %d", i), t, types); err != nil {
			return err
		}
	}
	return nil
}

func validateDefinedTypeDescriptors(types []DefinedTypeDescriptor) error {
	if len(types) == 0 {
		return nil
	}
	group := uint32(0)
	validateValue := func(context string, t ValueTypeDescriptor) error {
		return validateValueTypeDescriptor(context, t, types)
	}
	validateField := func(context string, f FieldTypeDescriptor) error {
		if f.Storage.Packed {
			if f.Storage.PackedType > PackedTypeI16 {
				return fmt.Errorf("compiled metadata invalid: %s packed type %d invalid", context, f.Storage.PackedType)
			}
			if f.Storage.Value != (ValueTypeDescriptor{}) {
				return fmt.Errorf("compiled metadata invalid: %s packed field carries value metadata", context)
			}
			return nil
		}
		return validateValue(context, f.Storage.Value)
	}
	for i, d := range types {
		if i == 0 {
			if d.RecGroup != 0 {
				return fmt.Errorf("compiled metadata invalid: type 0 recursive group %d, want 0", d.RecGroup)
			}
		} else if d.RecGroup < group || d.RecGroup > group+1 {
			return fmt.Errorf("compiled metadata invalid: type %d recursive group %d does not follow %d", i, d.RecGroup, group)
		}
		group = d.RecGroup
		for _, x := range d.Supers {
			if int(x) >= len(types) {
				return fmt.Errorf("compiled metadata invalid: type %d supertype %d out of range", i, x)
			}
		}
		if d.HasDescribes && int(d.Describes) >= len(types) {
			return fmt.Errorf("compiled metadata invalid: type %d describes index %d out of range", i, d.Describes)
		}
		if d.HasDescriptor && int(d.Descriptor) >= len(types) {
			return fmt.Errorf("compiled metadata invalid: type %d descriptor index %d out of range", i, d.Descriptor)
		}
		switch d.Kind {
		case CompositeTypeFunction:
			if len(d.Fields) != 0 || d.Array != (FieldTypeDescriptor{}) {
				return fmt.Errorf("compiled metadata invalid: function type %d carries field metadata", i)
			}
			for j, t := range d.Params {
				if err := validateValue(fmt.Sprintf("type %d param %d", i, j), t); err != nil {
					return err
				}
			}
			for j, t := range d.Results {
				if err := validateValue(fmt.Sprintf("type %d result %d", i, j), t); err != nil {
					return err
				}
			}
		case CompositeTypeStruct:
			if len(d.Params) != 0 || len(d.Results) != 0 || d.Array != (FieldTypeDescriptor{}) {
				return fmt.Errorf("compiled metadata invalid: struct type %d carries non-field metadata", i)
			}
			for j, f := range d.Fields {
				if err := validateField(fmt.Sprintf("type %d field %d", i, j), f); err != nil {
					return err
				}
			}
		case CompositeTypeArray:
			if len(d.Params) != 0 || len(d.Results) != 0 || len(d.Fields) != 0 {
				return fmt.Errorf("compiled metadata invalid: array type %d carries non-array metadata", i)
			}
			if err := validateField(fmt.Sprintf("type %d array field", i), d.Array); err != nil {
				return err
			}
		default:
			return fmt.Errorf("compiled metadata invalid: type %d composite kind %d invalid", i, d.Kind)
		}
	}
	return nil
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
