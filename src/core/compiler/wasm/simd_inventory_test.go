package wasm

import (
	"slices"
	"testing"
)

func TestSIMDNoImmediateSignatures(t *testing.T) {
	tests := []struct {
		sub     uint32
		inputs  []ValType
		results []ValType
	}{
		{15, []ValType{I32}, []ValType{V128}},
		{83, []ValType{V128}, []ValType{I32}},
		{228, []ValType{V128, V128}, []ValType{V128}},
		{261, []ValType{V128, V128, V128}, []ValType{V128}},
	}
	for _, tc := range tests {
		inputs, results, ok := SIMDNoImmediateSignature(tc.sub)
		if !ok || !slices.Equal(inputs, tc.inputs) || !slices.Equal(results, tc.results) {
			t.Fatalf("subopcode %d signature inputs=%v results=%v ok=%v", tc.sub, inputs, results, ok)
		}
	}
	if _, _, ok := SIMDNoImmediateSignature(13); ok {
		t.Fatal("shuffle immediate reported as no-immediate SIMD")
	}
}

func TestSIMDOpcodeInventory(t *testing.T) {
	seenSub := make(map[uint32]InstrKind, 256)
	seenKind := make(map[InstrKind]uint32, 256)
	add := func(sub uint32, kind InstrKind) {
		t.Helper()
		if prev, ok := seenSub[sub]; ok {
			t.Fatalf("SIMD subopcode %d maps to both %v and %v", sub, prev, kind)
		}
		if prev, ok := seenKind[kind]; ok {
			t.Fatalf("SIMD instruction %v maps to both %d and %d", kind, prev, sub)
		}
		seenSub[sub], seenKind[kind] = kind, sub
	}
	add(12, InstrV128Const)
	add(13, InstrI8x16Shuffle)
	for sub, kind := range fdMem {
		add(sub, kind)
	}
	for sub, kind := range fdLane {
		add(sub, kind)
	}
	for sub, kind := range fdNoImm {
		add(sub, kind)
	}

	if got, want := len(seenSub), 256; got != want {
		t.Fatalf("SIMD instruction count = %d, want %d", got, want)
	}
	relaxed := 0
	for sub := range seenSub {
		if sub >= 256 && sub <= 275 {
			relaxed++
		}
	}
	if relaxed != 20 {
		t.Fatalf("relaxed SIMD instruction count = %d, want 20", relaxed)
	}
	if core := len(seenSub) - relaxed; core != 236 {
		t.Fatalf("core SIMD instruction count = %d, want 236", core)
	}
	for sub := uint32(256); sub <= 275; sub++ {
		if _, ok := seenSub[sub]; !ok {
			t.Fatalf("relaxed SIMD subopcode %d missing", sub)
		}
	}
	for sub := uint32(0); sub <= 512; sub++ {
		_, want := seenSub[sub]
		if got := SIMDSubopcodeValid(sub); got != want {
			t.Fatalf("SIMDSubopcodeValid(%d) = %v, inventory = %v", sub, got, want)
		}
	}
}
