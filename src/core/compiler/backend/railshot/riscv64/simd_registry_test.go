//go:build riscv64

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSWARSIMDRegistryMatchesDecoder(t *testing.T) {
	count := 0
	for sub := uint32(0); sub <= 512; sub++ {
		got := swarSIMDSubopcodeValid(sub)
		want := wasm.SIMDSubopcodeValid(sub)
		if got != want {
			t.Errorf("SIMD subopcode %d: SWAR registry=%v decoder=%v", sub, got, want)
		}
		if got {
			count++
		}
	}
	if count != 256 {
		t.Fatalf("SWAR SIMD registry contains %d instructions, want 256", count)
	}
}
