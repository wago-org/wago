package wasm

import (
	"testing"

	"github.com/wago-org/wago/internal/buildfeatures"
)

func TestPackedInstructionNames(t *testing.T) {
	if got, want := len(instrKindNameOffsets), int(numInstrKinds)+1; got != want {
		t.Fatalf("offset count = %d, want %d", got, want)
	}
	if got := int(instrKindNameOffsets[len(instrKindNameOffsets)-1]); got != len(instrKindNameBlob) {
		t.Fatalf("final offset = %d, blob bytes = %d", got, len(instrKindNameBlob))
	}
	for _, test := range []struct {
		kind    InstrKind
		name    string
		enabled bool
	}{
		{InstrInvalid, "Invalid", true},
		{InstrMemoryAtomicNotify, "MemoryAtomicNotify", buildfeatures.Threads != 0},
		{InstrF64Copysign, "F64Copysign", true},
		{InstrStructNewDefaultDesc, "StructNewDefaultDesc", buildfeatures.GC != 0},
		{InstrV128Load64Lane, "V128Load64Lane", buildfeatures.SIMD != 0},
		{InstrI32x4RelaxedDotI8x16I7x16AddS, "I32x4RelaxedDotI8x16I7x16AddS", buildfeatures.RelaxedSIMD != 0},
		{InstrKind(uint16(numInstrKinds) + 100), "Invalid", false},
	} {
		want := "Invalid"
		if test.enabled {
			want = test.name
		}
		if got := test.kind.String(); got != want {
			t.Errorf("InstrKind(%d).String() = %q, want %q", test.kind, got, want)
		}
	}

	seen := make(map[string]InstrKind, numInstrKinds)
	for kind := InstrKind(0); kind < numInstrKinds; kind++ {
		name := kind.String()
		if buildfeatures.Custom && name == "Invalid" {
			continue
		}
		want := instrKindNameBlob[instrKindNameOffsets[kind]:instrKindNameOffsets[kind+1]]
		if name != want {
			t.Fatalf("InstrKind(%d) = %q, packed name = %q", kind, name, want)
		}
		if prior, ok := seen[name]; ok {
			t.Fatalf("InstrKind(%d) and InstrKind(%d) share name %q", prior, kind, name)
		}
		seen[name] = kind
	}
}
