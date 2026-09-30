//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import "testing"

func TestFuncSigLocalRegABIResultBanks(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  FuncSig
		want bool
	}{
		{"mixed pair", FuncSig{Params: []ValType{ValI32, ValF32}, Results: []ValType{ValI32, ValF32}}, preparedDirectFloatSupported},
		{"float pair", FuncSig{Params: []ValType{ValF32, ValF64}, Results: []ValType{ValF32, ValF64}}, preparedDirectFloatSupported},
		{"int quad", FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32, ValI64, ValI32, ValI64}}, preparedDirectWideSupported},
		{"float quad", FuncSig{Params: []ValType{ValF32}, Results: []ValType{ValF32, ValF64, ValF32, ValF64}}, preparedDirectWideSupported && preparedDirectFloatSupported},
		{"mixed quad", FuncSig{Params: []ValType{ValI32, ValF32}, Results: []ValType{ValI32, ValF32, ValI64, ValF64}}, preparedDirectWideSupported && preparedDirectFloatSupported},
		{"mixed three GP", FuncSig{Params: []ValType{ValI32, ValF32}, Results: []ValType{ValI32, ValI64, ValI32, ValF64}}, false},
		{"mixed five results", FuncSig{Params: []ValType{ValI32, ValF32}, Results: []ValType{ValI32, ValF32, ValI64, ValF64, ValI32}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := funcSigLocalRegABI(tc.sig); got != tc.want {
				t.Fatalf("funcSigLocalRegABI(%v) = %v, want %v", tc.sig, got, tc.want)
			}
		})
	}
}

func TestFuncSigIntRegABI(t *testing.T) {
	// Keep the eight-parameter integer ABI distinct from the seven-GPR local
	// ABI. Check every position, including result-only signatures.
	for params := 0; params <= 9; params++ {
		for results := 0; results <= 3; results++ {
			sig := FuncSig{Params: make([]ValType, params), Results: make([]ValType, results)}
			for i := range sig.Params {
				sig.Params[i] = ValI32
			}
			for i := range sig.Results {
				sig.Results[i] = ValI64
			}
			want := params <= 8 && results <= 2
			if got := funcSigIntRegABI(sig); got != want {
				t.Fatalf("params=%d results=%d: got %v, want %v", params, results, got, want)
			}
			for part, types := range [][]ValType{sig.Params, sig.Results} {
				for i, original := range types {
					// All byte-valued types, including unknown encodings, must be
					// rejected unless they are one of the two integer types.
					for value := 0; value < 256; value++ {
						typ := ValType(value)
						types[i] = typ
						expected := want && (typ == ValI32 || typ == ValI64)
						if got := funcSigIntRegABI(sig); got != expected {
							t.Fatalf("params=%d results=%d part=%d index=%d type=%#x: got %v, want %v", params, results, part, i, typ, got, expected)
						}
					}
					types[i] = original
				}
			}
		}
	}
}

func TestFuncSigIntRegABINoAlloc(t *testing.T) {
	sig := FuncSig{
		Params:  []ValType{ValI32, ValI64, ValI32, ValI64, ValI32, ValI64, ValI32, ValI64},
		Results: []ValType{ValI32, ValI64},
	}
	var accepted bool
	allocs := testing.AllocsPerRun(1000, func() { accepted = funcSigIntRegABI(sig) })
	if !accepted {
		t.Fatal("integer signature at the ABI limit was rejected")
	}
	if allocs != 0 {
		t.Fatalf("signature classification allocated %g times per call, want 0", allocs)
	}
}

func BenchmarkFuncSigIntRegABI(b *testing.B) {
	sig := FuncSig{
		Params:  []ValType{ValI32, ValI64, ValI32, ValI64, ValI32, ValI64, ValI32, ValI64},
		Results: []ValType{ValI32, ValI64},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !funcSigIntRegABI(sig) {
			b.Fatal("integer signature at the ABI limit was rejected")
		}
	}
}
