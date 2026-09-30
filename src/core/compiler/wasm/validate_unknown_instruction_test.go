package wasm

import "testing"

func TestValidateRejectsUnknownInstructionWithoutPanic(t *testing.T) {
	for _, kind := range []InstrKind{InstrKind(len(opEffects)), ^InstrKind(0)} {
		t.Run(kind.String(), func(t *testing.T) {
			defer func() {
				if got := recover(); got != nil {
					t.Errorf("validation panicked for instruction %d: %v", kind, got)
				}
			}()
			expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: kind}), ErrUnsupportedValidationOpcode)
			m := &Module{Globals: []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: kind}}}}}}
			if err := ValidateModule(m); err == nil {
				t.Fatalf("constant expression accepted instruction %d", kind)
			}
		})
	}
}

func BenchmarkValidateKnownInstruction(b *testing.B) {
	m := modWithFunc(nil, nil, Instruction{Kind: InstrNop})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateModule(m); err != nil {
			b.Fatal(err)
		}
	}
}
