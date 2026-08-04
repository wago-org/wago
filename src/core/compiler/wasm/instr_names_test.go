package wasm

import "testing"

func TestPackedInstructionNames(t *testing.T) {
	if got, want := len(instrKindNameOffsets), int(numInstrKinds)+1; got != want {
		t.Fatalf("offset count = %d, want %d", got, want)
	}
	if got := int(instrKindNameOffsets[len(instrKindNameOffsets)-1]); got != len(instrKindNameBlob) {
		t.Fatalf("final offset = %d, blob bytes = %d", got, len(instrKindNameBlob))
	}
	for kind, want := range map[InstrKind]string{
		InstrInvalid:                           "Invalid",
		InstrMemoryAtomicNotify:                "MemoryAtomicNotify",
		InstrF64Copysign:                       "F64Copysign",
		InstrStructNewDefaultDesc:              "StructNewDefaultDesc",
		InstrV128Load64Lane:                    "V128Load64Lane",
		InstrI32x4RelaxedDotI8x16I7x16AddS:     "I32x4RelaxedDotI8x16I7x16AddS",
		InstrKind(uint16(numInstrKinds) + 100): "Invalid",
	} {
		if got := kind.String(); got != want {
			t.Errorf("InstrKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
	seen := make(map[string]InstrKind, numInstrKinds)
	for kind := InstrKind(0); kind < numInstrKinds; kind++ {
		name := kind.String()
		if name == "" {
			t.Fatalf("InstrKind(%d) has empty name", kind)
		}
		if prior, ok := seen[name]; ok {
			t.Fatalf("InstrKind(%d) and InstrKind(%d) share name %q", prior, kind, name)
		}
		seen[name] = kind
	}
}
