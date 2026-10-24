//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInterruptibleMemoryFreeLoopFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{
		0x01, 0x01, 0x7f, // one declared i32 local; excludes caller-pin-preserving leaf ABI
		0x03, 0x40, // loop
		0x20, 0x00, // local.get 0
		0x41, 0x01, 0x6b, // i32.sub 1
		0x22, 0x00, // local.tee 0
		0x0d, 0x00, // br_if 0
		0x0b,       // end loop
		0x20, 0x00, // local.get 0
		0x0b, // end function
	}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Interruptible: true,
		Optimizations: map[string]bool{},
		Stats:         &stats,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Peephole["loop-trap-cell"]; got != 0 {
		t.Fatalf("memory-free loop cache count = %d, want 0", got)
	}
}
