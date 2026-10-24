package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// ABIType reports the one-slot public/product ABI category for values that the
// current runtime can describe. types is the containing module's flattened type
// graph and is consulted for indexed references. Struct/array references use
// ValAnyRef metadata slots. Non-null ingress remains fail-closed; the exact
// staged basic-struct result may egress only through a bounded store-owned token.
//
//go:noinline
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

func newWasmTypeDescriptorConverter(m *wasm.Module) wasmTypeDescriptorConverter {
	c := wasmTypeDescriptorConverter{m: m}
	if m != nil {
		c.groupCount = len(m.Types)
		if len(m.Types) > wasmTypeDescriptorInlineGroups {
			c.groupAt = make([]uint32, len(m.Types)+1)
		}
		for i := range m.Types {
			if c.groupAt != nil {
				c.groupAt[i+1] = c.groupAt[i] + uint32(len(m.Types[i].SubTypes))
			} else {
				c.inlineGroupAt[i+1] = c.inlineGroupAt[i] + uint32(len(m.Types[i].SubTypes))
			}
		}
	}
	return c
}

func (c wasmTypeDescriptorConverter) groupOffset(group int) uint32 {
	if c.groupAt != nil {
		return c.groupAt[group]
	}
	return c.inlineGroupAt[group]
}

//go:noinline
func (c wasmTypeDescriptorConverter) abiType(t wasm.ValType, types []DefinedTypeDescriptor) (ValType, error) {
	if abi, ok := primitiveABIType(t); ok {
		return abi, nil
	}
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

//go:noinline
func (c wasmTypeDescriptorConverter) abiTypes(ts []wasm.ValType, types []DefinedTypeDescriptor) ([]ValType, error) {
	out := make([]ValType, len(ts))
	if err := c.abiTypesInto(out, ts, types); err != nil {
		return nil, err
	}
	return out, nil
}

//go:noinline
func (c wasmTypeDescriptorConverter) abiTypesInto(out []ValType, ts []wasm.ValType, types []DefinedTypeDescriptor) error {
	if len(out) != len(ts) {
		return fmt.Errorf("ABI type destination length %d, want %d", len(out), len(ts))
	}
	for i := range ts {
		if abi, ok := primitiveABIType(ts[i]); ok {
			out[i] = abi
			continue
		}
		exact, err := c.valueType(ts[i], -1)
		if err != nil {
			return fmt.Errorf("value %d: %w", i, err)
		}
		abi, ok := exact.ABIType(types)
		if !ok {
			return fmt.Errorf("value %d: structural type is outside the current public ABI", i)
		}
		out[i] = abi
	}
	return nil
}

// Primitive ABI carriers have no module-dependent reference identity. Avoid
// constructing and immediately unpacking their full structural descriptors.
func primitiveABIType(t wasm.ValType) (ValType, bool) {
	switch t {
	case wasm.I32:
		return ValI32, true
	case wasm.I64:
		return ValI64, true
	case wasm.F32:
		return ValF32, true
	case wasm.F64:
		return ValF64, true
	case wasm.V128:
		return ValV128, true
	default:
		return 0, false
	}
}

func (c wasmTypeDescriptorConverter) typeDescriptors() ([]DefinedTypeDescriptor, error) {
	if c.m == nil {
		return nil, fmt.Errorf("nil wasm module")
	}
	out := make([]DefinedTypeDescriptor, 0, c.groupOffset(c.groupCount))
	valueCount := 0
	for gi := range c.m.Types {
		for si := range c.m.Types[gi].SubTypes {
			comp := &c.m.Types[gi].SubTypes[si].Comp
			if comp.Kind == wasm.CompFunc {
				valueCount += len(comp.Params) + len(comp.Results)
			}
		}
	}
	values := make([]ValueTypeDescriptor, valueCount)
	valueAt := 0
	descriptorGroup := uint32(0)
	for gi := range c.m.Types {
		if len(c.m.Types[gi].SubTypes) == 0 {
			continue
		}
		for si := range c.m.Types[gi].SubTypes {
			st := &c.m.Types[gi].SubTypes[si]
			var params, results []ValueTypeDescriptor
			if st.Comp.Kind == wasm.CompFunc {
				end := valueAt + len(st.Comp.Params)
				params = values[valueAt:end:end]
				valueAt = end
				end = valueAt + len(st.Comp.Results)
				results = values[valueAt:end:end]
				valueAt = end
			}
			d, err := c.definedType(st, gi, descriptorGroup, params, results)
			if err != nil {
				return nil, fmt.Errorf("type %d: %w", len(out), err)
			}
			out = append(out, d)
		}
		descriptorGroup++
	}
	return out, nil
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

// internCompiled retains the recent-entry fast path while routing misses
// through the compiled module's indexed interner. This keeps first-seen IDs
// stable without reverting to a full pool scan for every unique descriptor.
func (index *valueTypeInterner) internCompiled(c *Compiled, t ValueTypeDescriptor) uint32 {
	if c == nil {
		return 0
	}
	large := len(c.ValueTypes) >= 32
	if large && uint64(*index) < uint64(len(c.ValueTypes)) && c.ValueTypes[*index] == t {
		return uint32(*index)
	}
	var i uint32
	if len(c.ValueTypes) < 128 {
		// Keep the small-pool scan in the production interner's call frame.
		// Larger pools retain the lazy exact index and stable insertion order.
		i = internValueType(&c.ValueTypes, t)
	} else {
		i = c.internExactValueType(t)
	}
	if large || len(c.ValueTypes) >= 32 {
		*index = valueTypeInterner(i)
	}
	return i
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

func cloneDefinedTypeDescriptorsAndValues(in []DefinedTypeDescriptor, trailingValues []ValueTypeDescriptor) ([]DefinedTypeDescriptor, []ValueTypeDescriptor) {
	var superCount, valueTypeCount, fieldCount int
	for i := range in {
		superCount += len(in[i].Supers)
		valueTypeCount += len(in[i].Params) + len(in[i].Results)
		fieldCount += len(in[i].Fields)
	}
	supers := packedCloneStorage[uint32]{values: make([]uint32, superCount)}
	valueTypes := packedCloneStorage[ValueTypeDescriptor]{values: make([]ValueTypeDescriptor, valueTypeCount+len(trailingValues))}
	fields := packedCloneStorage[FieldTypeDescriptor]{values: make([]FieldTypeDescriptor, fieldCount)}
	out := make([]DefinedTypeDescriptor, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Supers = supers.clone(in[i].Supers)
		out[i].Params = valueTypes.clone(in[i].Params)
		out[i].Results = valueTypes.clone(in[i].Results)
		out[i].Fields = fields.clone(in[i].Fields)
	}
	return out, valueTypes.clone(trailingValues)
}

func (c wasmTypeDescriptorConverter) definedType(st *wasm.SubType, sourceGroup int, descriptorGroup uint32, params, results []ValueTypeDescriptor) (DefinedTypeDescriptor, error) {
	d := DefinedTypeDescriptor{RecGroup: descriptorGroup, Final: st.Final}
	for _, idx := range st.Supers {
		x, err := c.typeIndex(idx, sourceGroup)
		if err != nil {
			return d, wrapContextError("supertype", err)
		}
		d.Supers = append(d.Supers, x)
	}
	if describes, present := st.Metadata.Describes.Get(); present {
		x, err := c.typeIndex(describes, sourceGroup)
		if err != nil {
			return d, wrapContextError("describes", err)
		}
		d.HasDescribes, d.Describes = true, x
	}
	if descriptor, present := st.Metadata.Descriptor.Get(); present {
		x, err := c.typeIndex(descriptor, sourceGroup)
		if err != nil {
			return d, wrapContextError("descriptor", err)
		}
		d.HasDescriptor, d.Descriptor = true, x
	}
	switch st.Comp.Kind {
	case wasm.CompFunc:
		d.Kind = CompositeTypeFunction
		d.Params = params
		if err := c.valueTypesInto(d.Params, st.Comp.Params, sourceGroup); err != nil {
			return d, wrapContextError("params", err)
		}
		d.Results = results
		if err := c.valueTypesInto(d.Results, st.Comp.Results, sourceGroup); err != nil {
			return d, wrapContextError("results", err)
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
			return d, wrapContextError("array field", err)
		}
		d.Array = f
	default:
		return d, fmt.Errorf("unknown composite type kind %d", st.Comp.Kind)
	}
	return d, nil
}

func (c wasmTypeDescriptorConverter) valueTypes(ts []wasm.ValType, group int) ([]ValueTypeDescriptor, error) {
	out := make([]ValueTypeDescriptor, len(ts))
	if err := c.valueTypesInto(out, ts, group); err != nil {
		return nil, err
	}
	return out, nil
}

//go:noinline
func (c wasmTypeDescriptorConverter) valueTypesInto(out []ValueTypeDescriptor, ts []wasm.ValType, group int) error {
	if len(out) != len(ts) {
		return fmt.Errorf("value type destination length %d, want %d", len(out), len(ts))
	}
	for i := range ts {
		v, err := c.valueType(ts[i], group)
		if err != nil {
			return fmt.Errorf("value %d: %w", i, err)
		}
		out[i] = v
	}
	return nil
}

//go:noinline
func (c wasmTypeDescriptorConverter) valueType(t wasm.ValType, group int) (ValueTypeDescriptor, error) {
	switch t.Kind() {
	case wasm.ValNum:
		switch t.Num() {
		case wasm.NumI32:
			return ValueTypeDescriptor{Kind: ValueTypeI32}, nil
		case wasm.NumI64:
			return ValueTypeDescriptor{Kind: ValueTypeI64}, nil
		case wasm.NumF32:
			return ValueTypeDescriptor{Kind: ValueTypeF32}, nil
		case wasm.NumF64:
			return ValueTypeDescriptor{Kind: ValueTypeF64}, nil
		default:
			return ValueTypeDescriptor{}, fmt.Errorf("unknown numeric type 0x%x", byte(t.Num()))
		}
	case wasm.ValVec:
		return ValueTypeDescriptor{Kind: ValueTypeV128}, nil
	case wasm.ValRef:
		r, err := c.refType(t.Ref(), group)
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: r}, err
	default:
		return ValueTypeDescriptor{}, fmt.Errorf("unsupported value type kind %d", t.Kind())
	}
}

//go:noinline
func (c wasmTypeDescriptorConverter) refType(t wasm.RefType, group int) (ReferenceTypeDescriptor, error) {
	out := ReferenceTypeDescriptor{Nullable: t.Nullable(), Exact: t.Exact()}
	heap := t.Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		a, ok := abstractHeapTypeFromWasm(heap.Abs())
		if !ok {
			return out, fmt.Errorf("unknown abstract heap type 0x%x", byte(heap.Abs()))
		}
		out.Heap.Abstract = a
	case wasm.HeapTypeIndex:
		x, err := c.typeIndex(heap.Type(), group)
		if err != nil {
			return out, err
		}
		out.Heap.Defined, out.Heap.TypeIndex = true, x
	case wasm.HeapDefType:
		groupIndex, memberIndex, _, valid := heap.Def()
		if c.m == nil || !valid || int(groupIndex) >= c.groupCount || memberIndex >= uint32(len(c.m.Types[groupIndex].SubTypes)) {
			return out, fmt.Errorf("unknown defined heap type")
		}
		out.Heap.Defined = true
		out.Heap.TypeIndex = c.groupOffset(int(groupIndex)) + memberIndex
	default:
		return out, fmt.Errorf("unknown heap type kind %d", heap.Kind())
	}
	return out, nil
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
