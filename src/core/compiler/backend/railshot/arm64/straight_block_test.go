//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestStraightBlockRetainsOperandResidency(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0, 0x20, 0, 0x02, 0x7f, 0x20, 1, 0x41, 7, 0x36, 0, 0, 0x20, 1, 0x28, 0, 0, 0x0b, 0x6a, 0x0b,
	})
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"straight-line-block": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		want := 0
		if enabled {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["straight-line-block"]; got != want {
			t.Fatalf("enabled=%t straight blocks=%d, want %d", enabled, got, want)
		}
	}
}

func TestStraightBlockContainsNativeFill(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0, 0x20, 0, 0x02, 0x7f, 0x20, 1, 0x41, 7, 0x41, 33, 0xfc, 11, 0, 0x41, 7, 0x0b, 0x6a, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	c.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["straight-line-block"]; got != 1 {
		t.Fatalf("fill blocks=%d want 1", got)
	}
}

func TestStraightBlockContainsNativeAtomics(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0, 0x20, 0, 0x02, 0x7f, 0x20, 1, 0x41, 7, 0xfe, 0x17, 2, 0, 0x20, 1, 0x41, 1, 0xfe, 0x1e, 2, 0, 0x0b, 0x6a, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories[0].Shared = true
	m.Memories[0].Limits.HasMax = true
	m.Memories[0].Limits.Max = 2
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"straight-line-block": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		want := 0
		if enabled {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["straight-line-block"]; got != want {
			t.Fatalf("enabled=%t native atomic blocks=%d want=%d", enabled, got, want)
		}
	}
}
