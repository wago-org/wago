//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/big"
	"testing"
)

func TestIntegerParitySumLoweringUnsignedExtrema(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x41, 1, 0x71, 0x04, 0x7f, 0x20, 2, 0x41, 3, 0x6c, 0x05, 0x20, 2, 0x41, 7, 0x6a, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	c.CodeImage.Close()
	if stats.Funcs[0].Peephole["integer-parity-sum-loop"] != 1 {
		t.Fatal("periodic reduction not selected")
	}
	for _, n := range []uint64{0, 1, 2, 3, 65535, 65536, 1 << 31, (1 << 31) + 1, (1 << 32) - 1} {
		count := n
		if count == 0 {
			count = 1
		}
		evens, odds := (count+1)/2, count/2
		// Sum each arithmetic progression by (first+last)*count/2 in arbitrary
		// precision, then reduce once. This independently checks intermediate wrap.
		even := new(big.Int).SetUint64(2*(evens-1) + 14)
		even.Mul(even, new(big.Int).SetUint64(evens))
		even.Quo(even, big.NewInt(2))
		odd := new(big.Int).SetUint64(6 * odds)
		odd.Mul(odd, new(big.Int).SetUint64(odds))
		odd.Quo(odd, big.NewInt(2))
		want := uint32(new(big.Int).Add(even, odd).Uint64()) + uint32(0xfedcba98)
		got := func() uint64 {
			v, err := runArm64Wrapper(t, m, n, 0xfedcba98)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}()
		if uint32(got) != want {
			t.Fatalf("n=%x got=%x want=%x", n, got, want)
		}
	}
	for name, opt := range map[string]CompileOptions{"disabled": {Optimizations: map[string]bool{"integer-sum-loop": false}}, "interruptible": {Interruptible: true}} {
		var stats ModuleStats
		opt.Stats = &stats
		c, err := CompileModuleWith(m, opt)
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		if stats.Funcs[0].Peephole["integer-parity-sum-loop"] != 0 {
			t.Fatalf("%s selected reduction", name)
		}
	}
}
