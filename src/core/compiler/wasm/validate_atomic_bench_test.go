package wasm

import "testing"

func BenchmarkValidateAtomicEffects(b *testing.B) {
	body := make([]Instruction, 0, 7*3*32)
	loads := []InstrKind{
		InstrI32AtomicLoad,
		InstrI64AtomicLoad,
		InstrI32AtomicLoad8U,
		InstrI32AtomicLoad16U,
		InstrI64AtomicLoad8U,
		InstrI64AtomicLoad16U,
		InstrI64AtomicLoad32U,
	}
	for range 32 {
		for _, kind := range loads {
			body = append(body,
				Instruction{Kind: InstrI32Const},
				Instruction{Kind: kind},
				Instruction{Kind: InstrDrop},
			)
		}
	}
	mod := modWithFunc(nil, nil, body...)
	max := uint64(1)
	mod.Memories = []MemType{{Shared: true, Limits: Limits{Min: 1, Max: max, HasMax: true}}}
	b.ReportMetric(float64(len(loads)*32), "lookups/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateModule(mod); err != nil {
			b.Fatal(err)
		}
	}
}
