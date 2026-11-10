//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDLoadExtractUsesScalarAccess(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0xfd, 0, 4, 0, 0xfd, 0x1b, 3, 0x0b})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	c.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["simd-load-extract"]; got != 1 {
		t.Fatalf("narrow loads=%d, want 1", got)
	}
}
