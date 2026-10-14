//go:build amd64

package amd64

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
			got := runAmd64u(t, m, bound, 0xfedcba9876543210)
			if got != want.Uint64() {
				t.Fatalf("wide=%t bound=%x: %x; want %x", wide, bound, got, want.Uint64())
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

func TestIntegerPopcountSumZeroAccumulatorKeepsI32Result(t *testing.T) {
	requireCompilerDiagnostics(t)
	// The accumulator is a known-zero local, allowing the final add to fold
	// away. A 64-bit intermediate must still return its canonical i32 value.
	body := []byte{1, 2, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x69, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if stats.Funcs[0].Peephole["integer-popcount-sum-loop"] != 1 {
		t.Fatal("popcount reduction not selected")
	}
	// All 2^32 values have 32*2^31 set bits in total. Omit UINT32_MAX.
	if got := runCompiledAmd64u(t, compiled, 1<<32-1); got != 0xffffffe0 {
		t.Fatalf("noncanonical i32 sum: %x; want ffffffe0", got)
	}
}
