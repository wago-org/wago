package wasm

import "testing"

func TestValidatedFuncFlagsTable(t *testing.T) {
	initValidatedFuncFlags()
	for kind := InstrKind(0); kind < numInstrKinds; kind++ {
		var facts ValidatedFuncFacts
		facts.observeSlow(kind)
		if got, want := validatedFuncFlagsByKind[kind], facts.Flags; got != want {
			t.Fatalf("kind %d flags = %#x, want %#x", kind, got, want)
		}
	}
}
