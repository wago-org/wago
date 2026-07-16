package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTypeDescriptorsPreserveRecursiveReferenceStructure(t *testing.T) {
	recRef := func(index uint32) wasm.ValType {
		return wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: index, Rec: true}), true))
	}
	absRef := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	describes := wasm.TypeIdx{Index: 0, Rec: true}
	descriptor := wasm.TypeIdx{Index: 1, Rec: true}
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}},
		{SubTypes: []wasm.SubType{
			{
				Supers:   []wasm.TypeIdx{{Index: 0}},
				Metadata: wasm.TypeMetadata{Describes: &describes, Descriptor: &descriptor},
				Comp: wasm.CompType{Kind: wasm.CompFunc,
					Params:  []wasm.ValType{recRef(0), absRef},
					Results: []wasm.ValType{recRef(1)},
				},
			},
			{
				Final: true,
				Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{
					{Storage: wasm.StorageType{Val: recRef(0)}, Mut: wasm.Var},
					{Storage: wasm.StorageType{Packed: true, Pack: wasm.PackI16}},
				}},
			},
		}},
	}}

	got, err := typeDescriptorsFromWasm(m)
	if err != nil {
		t.Fatalf("typeDescriptorsFromWasm: %v", err)
	}
	if len(got) != 3 || got[0].RecGroup != 0 || got[1].RecGroup != 1 || got[2].RecGroup != 1 {
		t.Fatalf("flattened groups = %#v", got)
	}
	fn := got[1]
	if fn.Kind != CompositeTypeFunction || len(fn.Supers) != 1 || fn.Supers[0] != 0 {
		t.Fatalf("function descriptor = %#v", fn)
	}
	if !fn.HasDescribes || fn.Describes != 1 || !fn.HasDescriptor || fn.Descriptor != 2 {
		t.Fatalf("descriptor metadata = describes(%v,%d) descriptor(%v,%d)", fn.HasDescribes, fn.Describes, fn.HasDescriptor, fn.Descriptor)
	}
	if ref := fn.Params[0].Ref; !ref.Exact || ref.Nullable || !ref.Heap.Defined || ref.Heap.TypeIndex != 1 {
		t.Fatalf("recursive param = %#v, want non-null exact type 1", ref)
	}
	if ref := fn.Params[1].Ref; !ref.Nullable || ref.Heap.TypeIndex != 0 {
		t.Fatalf("absolute param = %#v, want nullable type 0", ref)
	}
	if ref := fn.Results[0].Ref; ref.Heap.TypeIndex != 2 {
		t.Fatalf("recursive result = %#v, want type 2", ref)
	}
	st := got[2]
	if st.Kind != CompositeTypeStruct || len(st.Fields) != 2 || !st.Fields[0].Mutable || st.Fields[0].Storage.Value.Ref.Heap.TypeIndex != 1 {
		t.Fatalf("struct descriptor = %#v", st)
	}
	if !st.Fields[1].Storage.Packed || st.Fields[1].Storage.PackedType != PackedTypeI16 {
		t.Fatalf("packed field = %#v", st.Fields[1])
	}
}

func TestTypeDescriptorsCompactEmptyRecursiveGroups(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
		{},
		{},
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
	}}
	got, err := typeDescriptorsFromWasm(m)
	if err != nil {
		t.Fatalf("typeDescriptorsFromWasm: %v", err)
	}
	if len(got) != 2 || got[0].RecGroup != 0 || got[1].RecGroup != 1 {
		t.Fatalf("compacted recursive groups = %#v", got)
	}
	if err := validateDefinedTypeDescriptors(got); err != nil {
		t.Fatalf("compacted recursive groups rejected: %v", err)
	}
}

func TestTypeDescriptorsRejectMalformedRecursiveIndex(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{
		Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{
			wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1, Rec: true}), false)),
		}},
	}}}}}
	if _, err := typeDescriptorsFromWasm(m); err == nil || !strings.Contains(err.Error(), "recursive type index 1 out of range") {
		t.Fatalf("malformed recursive type error = %v", err)
	}
}

func TestValueTypeDescriptorABITypeKeepsReferenceCategories(t *testing.T) {
	indexed := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	types := []DefinedTypeDescriptor{{Kind: CompositeTypeFunction}}
	if got, ok := indexed.ABIType(types); !ok || got != ValFuncRef {
		t.Fatalf("indexed ABI type = %v, %v; want funcref,true", got, ok)
	}
	if got, ok := indexed.ABIType([]DefinedTypeDescriptor{{Kind: CompositeTypeStruct}}); ok {
		t.Fatalf("indexed struct ABI type = %v, true; want unsupported", got)
	}
	any := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	if got, ok := any.ABIType(nil); !ok || got != ValAnyRef {
		t.Fatalf("anyref ABI type = %v, %v; want anyref,true", got, ok)
	}
}
