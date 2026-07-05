package frontend

import "testing"

func TestDecodeValidateSIMDHeavyAllocationBudget(t *testing.T) {
	data := benchFrontendSIMDHeavyModuleBytes()
	allocs := testing.AllocsPerRun(50, func() {
		m, err := DecodeValidate(data)
		if err != nil {
			t.Fatalf("DecodeValidate: %v", err)
		}
		benchModuleSink = m
	})
	// Intentionally conservative: the SIMD-heavy DecodeValidate benchmark is
	// currently ~38 allocs/op on linux/amd64 Go 1.24. Keep the budget loose so
	// it catches obvious allocation cliffs rather than machine/version noise.
	const budget = 100.0
	if allocs > budget {
		t.Fatalf("allocations = %.1f, budget = %.1f", allocs, budget)
	}
}
