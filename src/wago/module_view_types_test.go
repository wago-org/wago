package wago

import "testing"

func TestModuleViewDefinedTypeReturnsDetachedDescriptor(t *testing.T) {
	compiled := &Compiled{Types: []DefinedTypeDescriptor{
		{
			Kind:   CompositeTypeFunction,
			Supers: []uint32{1},
			Params: []ValueTypeDescriptor{{Kind: ValueTypeI32}},
		},
		{
			Kind: CompositeTypeArray,
			Array: FieldTypeDescriptor{
				Storage: StorageTypeDescriptor{Packed: true, PackedType: PackedTypeI8},
				Mutable: true,
			},
		},
	}}
	view := ModuleView{compiled: compiled}

	got, ok := view.DefinedType(0)
	if !ok {
		t.Fatal("DefinedType(0) was not found")
	}
	if got.Kind != CompositeTypeFunction || len(got.Supers) != 1 || got.Supers[0] != 1 || len(got.Params) != 1 || got.Params[0].Kind != ValueTypeI32 {
		t.Fatalf("DefinedType(0) = %#v", got)
	}

	got.Supers[0] = 99
	got.Params[0].Kind = ValueTypeI64
	if compiled.Types[0].Supers[0] != 1 || compiled.Types[0].Params[0].Kind != ValueTypeI32 {
		t.Fatal("DefinedType returned slices aliasing compiled metadata")
	}

	array, ok := view.DefinedType(1)
	if !ok || array.Kind != CompositeTypeArray || !array.Array.Storage.Packed || array.Array.Storage.PackedType != PackedTypeI8 || !array.Array.Mutable {
		t.Fatalf("DefinedType(1) = %#v, %v", array, ok)
	}

	if _, ok := view.DefinedType(2); ok {
		t.Fatal("DefinedType accepted an out-of-range type index")
	}
	if _, ok := (ModuleView{}).DefinedType(0); ok {
		t.Fatal("nil ModuleView unexpectedly resolved a type")
	}
}
