package wago

import "testing"

func TestSignatureValidationOwnsNoTemporarySlices(t *testing.T) {
	types := []DefinedTypeDescriptor{{Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{{Kind: ValueTypeI32}, {Kind: ValueTypeV128}}, Results: []ValueTypeDescriptor{{Kind: ValueTypeI64}}}}
	sig := FuncSig{HasTypeIndex: true, Params: []ValType{ValI32, ValV128}, Results: []ValType{ValI64}}
	for _, indexed := range []bool{false, true} {
		sig.HasTypeIndex = indexed
		if n := testing.AllocsPerRun(100, func() {
			if err := validateFuncSignature(sig, types); err != nil {
				t.Fatal(err)
			}
		}); n != 0 {
			t.Fatalf("indexed=%v: %g allocations", indexed, n)
		}
	}
	params, _, err := exactFuncSignature(sig, types)
	if err != nil {
		t.Fatal(err)
	}
	params[0].Kind = ValueTypeF32
	if types[0].Params[0].Kind != ValueTypeI32 {
		t.Fatal("public result aliases exact metadata")
	}
	sig.Params[0] = ValF32
	if err := validateFuncSignature(sig, types); err == nil {
		t.Fatal("accepted ABI mismatch")
	}
	sig.TypeIndex = 1
	if err := validateFuncSignature(sig, types); err == nil {
		t.Fatal("accepted missing type")
	}
}

func BenchmarkValidateFuncSignature(b *testing.B) {
	types := []DefinedTypeDescriptor{{Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{{Kind: ValueTypeI32}}, Results: []ValueTypeDescriptor{{Kind: ValueTypeI64}}}}
	sig := FuncSig{HasTypeIndex: true, Params: []ValType{ValI32}, Results: []ValType{ValI64}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := validateFuncSignature(sig, types); err != nil {
			b.Fatal(err)
		}
	}
}
