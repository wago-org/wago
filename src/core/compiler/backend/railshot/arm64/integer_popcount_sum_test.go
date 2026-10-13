//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/big"
	"testing"
)

func TestIntegerPopcountSumUnsignedExtrema(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		typ, constant, add, less, pop := wasm.I32, byte(0x41), byte(0x6a), byte(0x49), byte(0x69)
		width := uint(32)
		if wide {
			typ, constant, add, less, pop = wasm.I64, 0x42, 0x7c, 0x54, 0x7b
			width = 64
		}
		localType := byte(0x7f)
		if wide {
			localType = 0x7e
		}
		body := []byte{1, 1, localType, 0x03, 0x40, 0x20, 1, 0x20, 2, pop, add, 0x21, 1, 0x20, 2, constant, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, less, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
		m := modFuncs(t, funcDef{params: []wasm.ValType{typ, typ}, results: []wasm.ValType{typ}, body: body})
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		if stats.Funcs[0].Peephole["integer-popcount-sum-loop"] != 1 {
			t.Fatalf("wide=%t: popcount reduction not selected", wide)
		}
		bounds := []uint64{0, 1, 2, 3, 255, 256, 257, 65535, 65536, 1 << 31, (1 << 32) - 1}
		if wide {
			bounds = append(bounds, 1<<32, 1<<63, (1<<63)+1, ^uint64(0))
		}
		for _, bound := range bounds {
			n := new(big.Int).SetUint64(max(bound, 1))
			want := new(big.Int).SetUint64(0xfedcba9876543210)
			// Independent per-bit cycle counts, not the highest-bit descent emitted.
			for bit := uint(0); bit < width; bit++ {
				half := new(big.Int).Lsh(big.NewInt(1), bit)
				cycle := new(big.Int).Lsh(big.NewInt(1), bit+1)
				q, r := new(big.Int), new(big.Int)
				q.QuoRem(n, cycle, r)
				want.Add(want, new(big.Int).Mul(q, half))
				r.Sub(r, half)
				if r.Sign() > 0 {
					want.Add(want, r)
				}
			}
			want.Mod(want, new(big.Int).Lsh(big.NewInt(1), width))
			got, err := runArm64Wrapper(t, m, bound, 0xfedcba9876543210)
			if err != nil || got != want.Uint64() {
				t.Fatalf("wide=%t bound=%x: %x, %v; want %x", wide, bound, got, err, want.Uint64())
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
			if stats.Funcs[0].Peephole["integer-popcount-sum-loop"] != 0 {
				t.Fatalf("%s selected reduction", name)
			}
		}
	}
}
