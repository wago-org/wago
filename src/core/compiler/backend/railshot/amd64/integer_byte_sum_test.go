//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIntegerByteSumUnsignedExtrema(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 2, 0xfd, 15, 0xfd, 12}
	for i := byte(16); i > 0; i-- {
		body = append(body, i-1)
	}
	body = append(body, 0xfd, 14, 0xfd, 22, 3, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	if stats.Funcs[0].Peephole["integer-byte-sum-loop"] != 1 {
		t.Fatal("unsigned byte reduction not selected")
	}
	for _, bound := range []uint64{0, 1, 2, 255, 256, 257, 65535, 65536, 1 << 31, 1<<32 - 1} {
		n := max(bound, 1)
		// Independent finite-period enumeration for the short residual.
		total := (n / 256) * 32640
		for i := uint64(0); i < n%256; i++ {
			total += i
		}
		for _, seed := range []uint64{0, 0xfedcba98} {
			want := uint64(uint32(seed + total))
			if got := runCompiledAmd64u(t, c, bound, seed); got != want {
				t.Fatalf("n=%x seed=%x: %x; want %x", bound, seed, got, want)
			}
		}
	}
}
