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
	const budget = 100.0 // measured ~40 on linux/amd64 Go 1.25; catches obvious regressions.
	if allocs > budget {
		t.Fatalf("allocations = %.1f, budget = %.1f", allocs, budget)
	}
}
