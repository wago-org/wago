//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestUnsignedMagicQuotientDirectDestination(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, d := range []uint64{3, 5, 10, 17, 100, 1000} {
		m := divConstMod(t, false, divOp{"i32.div_u", 0x6e, false, false}, d)
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		if got := stats.Funcs[0].Peephole["magic-quotient-direct"]; got != 1 {
			t.Fatalf("d=%d selections=%d want 1", d, got)
		}
	}
}

func TestUnsignedMagicQuotientLocalAssignment(t *testing.T) {
	for _, d := range []uint32{3, 5, 10, 17, 100, 1000, 0x7fffffff, 0xffffffff} {
		body := []byte{0, 0x20, 0, 0x41}
		body = append(body, wasmtest.SLEB32(int32(d))...)
		body = append(body, 0x6e, 0x21, 0, 0x20, 0, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		n := uint32(0x12345678)
		for range 64 {
			n ^= n << 13
			n ^= n >> 17
			n ^= n << 5
			input := uint64(n) | 0xdeadbeef00000000
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, input)
			if err != nil || got != uint64(n/d) {
				t.Fatalf("%d/%d=%d,%v want %d", n, d, got, err, n/d)
			}
		}
	}
}
