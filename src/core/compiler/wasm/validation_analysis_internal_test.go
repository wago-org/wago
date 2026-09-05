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

func TestValidatedFuncSegmentCountExactFallbackBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  InstrKind
		count func(ValidatedFuncFacts) uint8
	}{
		{"data", InstrDataDrop, func(f ValidatedFuncFacts) uint8 { return f.DataStateCount }},
		{"element", InstrElemDrop, func(f ValidatedFuncFacts) uint8 { return f.ElemStateCount }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var exact ValidatedFuncFacts
			exact.observeInstruction(&Instruction{Kind: tc.kind, Index: 254})
			if tc.count(exact) != 255 || exact.Flags&ValidatedFuncNeedsDetailedRequirements != 0 {
				t.Fatalf("exact segment summary = %#v, want count 255 without fallback", exact)
			}

			var overflow ValidatedFuncFacts
			overflow.observeInstruction(&Instruction{Kind: tc.kind, Index: 255})
			if tc.count(overflow) != 255 || overflow.Flags&ValidatedFuncNeedsDetailedRequirements == 0 {
				t.Fatalf("overflow segment summary = %#v, want saturated count and exact fallback", overflow)
			}
		})
	}
}
