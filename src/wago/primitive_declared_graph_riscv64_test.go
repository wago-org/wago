//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func rv64PrimitiveMixedGraph(t testing.TB) *Compiled {
	t.Helper()
	indexed := func(index uint32) wasm.ValType {
		return wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: index, Rec: true}), false))
	}
	field := wasm.NewFieldType(wasm.StorageVal(indexed(2)), wasm.Var)
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{indexed(0)}, Results: []wasm.ValType{wasm.I32}}},
			{HasPrefix: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{field}}},
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompArray, Array: wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wasm.Var)}},
		}},
		{SubTypes: []wasm.SubType{{Final: true, HasPrefix: true, Supers: []wasm.TypeIdx{{Index: 1}}, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 2}), false))), wasm.Var), wasm.NewFieldType(wasm.StorageVal(wasm.I64), wasm.Const)}}}}},
	}}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	types, err := typeDescriptorsFromWasm(m)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := m.StructuralTypeKeyChecked(0)
	if !ok {
		t.Fatal("source key missing")
	}
	sig := FuncSig{HasTypeIndex: true, TypeIndex: 0, Params: []ValType{ValFuncRef}, Results: []ValType{ValI32}}
	return &Compiled{Types: types, NumImports: 1, importFuncSigs: []FuncSig{sig}, Funcs: []FuncSig{sig}, FuncTypeID: []uint64{key, key}, requiredFeatures: CoreFeatureGC | CoreFeatureTypedFunctionReferences}
}

func TestRV64PrimitiveDeclaredGraphKeys(t *testing.T) {
	c := rv64PrimitiveMixedGraph(t)
	var keys []uint64
	if err := rv64ValidatePrimitiveFunctionKeys(c, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != len(c.Types) || keys[0] != c.FuncTypeID[0] || keys[1] != 0 || keys[2] != 0 || keys[3] != 0 {
		t.Fatalf("declared keys: %v", keys)
	}
	// An inert array is in the function's recursive group. Its packed width
	// must affect the function discriminator even though the ABI is unchanged.
	c.Types[2].Array.Storage.PackedType = PackedTypeI16
	if err := rv64ValidatePrimitiveFunctionKeys(c); err == nil {
		t.Fatal("changed recursive array accepted with old function keys")
	}
	m, err := rv64PrimitiveDeclaredGraph(c)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := m.StructuralTypeKeyChecked(0)
	if !ok || key == c.FuncTypeID[0] {
		t.Fatal("inert recursive member did not change key")
	}
	c.FuncTypeID[0], c.FuncTypeID[1] = key, key
	if err := rv64ValidatePrimitiveFunctionKeys(c); err != nil {
		t.Fatalf("valid changed graph rejected with matching keys: %v", err)
	}
}

func TestRV64PrimitiveDeclaredGraphRejectsInvalidBindings(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Compiled)
	}{
		{"short-keys", func(c *Compiled) { c.FuncTypeID = c.FuncTypeID[:1] }},
		{"extra-keys", func(c *Compiled) { c.FuncTypeID = append(c.FuncTypeID, 0) }},
		{"missing-import", func(c *Compiled) { c.importFuncSigs = nil }},
		{"negative-import-count", func(c *Compiled) { c.NumImports = -1 }},
		{"missing-type", func(c *Compiled) { c.Funcs[0].HasTypeIndex = false }},
		{"aggregate-binding", func(c *Compiled) { c.Funcs[0].TypeIndex = 1 }},
		{"unknown-binding", func(c *Compiled) { c.Funcs[0].TypeIndex = uint32(len(c.Types)) }},
		{"wrong-local-abi", func(c *Compiled) { c.Funcs[0].Params = []ValType{ValExternRef} }},
		{"wrong-import-abi", func(c *Compiled) { c.importFuncSigs[0].Results = nil }},
		{"wrong-key", func(c *Compiled) { c.FuncTypeID[1] ^= 1 }},
		{"final-super", func(c *Compiled) { c.Types[1].Final = true }},
		{"cross-kind-super", func(c *Compiled) { c.Types[3].Supers[0] = 0 }},
		{"unknown-field-reference", func(c *Compiled) { c.Types[1].Fields[0].Storage.Value.Ref.Heap.TypeIndex = uint32(len(c.Types)) }},
		{"invalid-packed-field", func(c *Compiled) { c.Types[2].Array.Storage.PackedType = PackedTypeI16 + 1 }},
		{"abstract-heap-index", func(c *Compiled) {
			c.Types[1].Fields[0].Storage.Value = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny, TypeIndex: 1}}}
		}},
		{"defined-heap-abstract", func(c *Compiled) { c.Types[1].Fields[0].Storage.Value.Ref.Heap.Abstract = AbstractHeapArray }},
		{"invalid-group", func(c *Compiled) { c.Types[2].RecGroup = 2 }},
		{"aggregate-signature", func(c *Compiled) { c.Types[0].Params[0].Ref.Heap.TypeIndex = 1 }},
		{"descriptor-extension", func(c *Compiled) { c.Types[1].HasDescriptor = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := rv64PrimitiveMixedGraph(t)
			test.change(c)
			if err := rv64ValidatePrimitiveFunctionKeys(c); err == nil {
				t.Fatal("invalid graph or function binding accepted")
			}
		})
	}
}

func BenchmarkRV64PrimitiveDeclaredGraphKeys(b *testing.B) {
	c := rv64PrimitiveMixedGraph(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := rv64ValidatePrimitiveFunctionKeys(c); err != nil {
			b.Fatal(err)
		}
	}
}
