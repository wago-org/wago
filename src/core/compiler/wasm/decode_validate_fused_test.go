package wasm

import "testing"

func fusedDecodeValidateForTest(data []byte, features ValidationFeatures) error {
	m, err := DecodeModuleForValidationWithFeatures(data, features)
	if err != nil {
		return err
	}
	return ValidateModuleWithFeatures(m, features)
}
func TestFusedDecodeValidateParity(t *testing.T) {
	features := ValidationFeatures{MultiMemory: true}
	for kind := uint8(0); kind < 7; kind++ {
		for _, funcs := range []uint8{1, 8, 32} {
			for mutation := uint8(0); mutation < 5; mutation++ {
				for _, arg := range []uint32{0, 5, 17, 23, 222} {
					data := mutateDifferentialModule(generatedDifferentialModule(kind, funcs), mutation, arg)
					want := byteBackedDecodeThenValidateWithFeatures(data, features)
					got := fusedDecodeValidateForTest(data, features)
					if (want == nil) != (got == nil) {
						t.Fatalf("kind=%d funcs=%d mutation=%d arg=%d: separate=%v fused=%v", kind, funcs, mutation, arg, want, got)
					}
				}
			}
		}
	}
}
func FuzzFusedDecodeValidate(f *testing.F) {
	for kind := uint8(0); kind < 7; kind++ {
		f.Add(generatedDifferentialModule(kind, 1))
	}
	f.Add([]byte("\x00asm\x01\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		features := ValidationFeatures{MultiMemory: true}
		want := byteBackedDecodeThenValidateWithFeatures(data, features)
		got := fusedDecodeValidateForTest(data, features)
		if (want == nil) != (got == nil) {
			t.Fatalf("separate=%v fused=%v", want, got)
		}
	})
}

func FuzzFusedDecodeValidateGenerated(f *testing.F) {
	for kind := uint8(0); kind < 7; kind++ {
		f.Add(kind, uint8(8), uint8(0), uint32(0))
	}
	f.Fuzz(func(t *testing.T, kind, funcs, mutation uint8, arg uint32) {
		data := mutateDifferentialModule(generatedDifferentialModule(kind, funcs), mutation, arg)
		features := ValidationFeatures{MultiMemory: true}
		want := byteBackedDecodeThenValidateWithFeatures(data, features)
		got := fusedDecodeValidateForTest(data, features)
		if (want == nil) != (got == nil) {
			t.Fatalf("separate=%v fused=%v", want, got)
		}
	})
}
