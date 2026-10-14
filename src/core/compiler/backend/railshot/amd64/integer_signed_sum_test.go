//go:build amd64

package amd64

import (
	"math/big"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIntegerSignedSumUnsignedExtrema(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, bits := range []uint8{8, 16, 32} {
		body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 2}
		negative := bits == 32
		if negative {
			body = append(body, 0xad, 0xc4, 0x42, 32, 0x88, 0xa7)
		} else if bits == 8 {
			body = append(body, 0xc0)
		} else {
			body = append(body, 0xc1)
		}
		body = append(body, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
		m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		if stats.Funcs[0].Peephole["integer-signed-sum-loop"] != 1 {
			t.Fatalf("bits=%d: signed reduction was not selected", bits)
		}
		for _, bound := range []uint64{0, 1, 127, 128, 255, 256, 65535, 65536, 1 << 31, (1 << 31) + 1, (1 << 32) - 1} {
			n := max(bound, 1)
			period := uint64(1) << bits
			want := big.NewInt(0)
			if negative {
				if n > period/2 {
					want.SetInt64(-int64(n - period/2))
				}
			} else {
				want.SetInt64(-int64(n/period) * int64(period/2))
				// Complete periods contribute -M/2. Check the short residual by
				// an independent signed scalar sequence, not Euclidean descent.
				for i := uint64(0); i < n%period; i++ {
					v := int64(i)
					if i >= period/2 {
						v -= int64(period)
					}
					want.Add(want, big.NewInt(v))
				}
			}
			want.Add(want, new(big.Int).SetUint64(0xfedcba98))
			want.Mod(want, new(big.Int).Lsh(big.NewInt(1), 32))
			got := runAmd64u(t, m, bound, 0xfedcba98)
			if got != want.Uint64() {
				t.Fatalf("bits=%d bound=%x: %x; want %x", bits, bound, got, want.Uint64())
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
			if stats.Funcs[0].Peephole["integer-signed-sum-loop"] != 0 {
				t.Fatalf("%s selected signed reduction", name)
			}
		}
	}
}

func TestIntegerSignedAffineSumMaximumBound(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, bits := range []uint8{8, 16, 32} {
		for _, coefficients := range [][2]int32{{13, -19}, {97, 0}, {-2147483647, 2147483647}, {0, -1}, {2, 42}, {1048583, 0}} {
			a, b := coefficients[0], coefficients[1]
			body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x41}
			body = append(body, wasmtest.SLEB32(a)...)
			body = append(body, 0x6c, 0x41)
			body = append(body, wasmtest.SLEB32(b)...)
			body = append(body, 0x6a)
			if bits == 32 {
				body = append(body, 0xad, 0xc4, 0x42, 32, 0x88, 0xa7)
			} else if bits == 8 {
				body = append(body, 0xc0)
			} else {
				body = append(body, 0xc1)
			}
			body = append(body, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
			m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
			var stats ModuleStats
			compiled, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			if stats.Funcs[0].Peephole["integer-signed-sum-loop"] != 1 {
				compiled.CodeImage.Close()
				t.Fatalf("bits=%d a=%d b=%d: signed reduction not selected", bits, a, b)
			}
			seed, bound := uint32(0xfedcba98), uint64(1<<32-1)
			var contribution uint64
			if bits == 32 {
				if a == 0 {
					if b < 0 {
						contribution = bound * uint64(^uint32(0))
					}
				} else {
					// Any nonconstant affine orbit modulo 2^32 visits the
					// negative half equally often. Remove the omitted final term
					// from the complete 2^32-element sequence.
					contribution = 1 << 31
					if int32(uint32(b)-uint32(a)) < 0 {
						contribution++
					}
				}
			} else {
				// Independent finite-period oracle: enumerate at most 65536
				// signed values, then count complete periods and the residual.
				modulus := uint64(1) << bits
				gcd := modulus
				if v := uint64(uint32(a)) & (modulus - 1); v != 0 {
					gcd = v & -v
				}
				period := modulus / gcd
				var cycle, tail uint64
				for i := uint64(0); i < period; i++ {
					x := uint32(i)*uint32(a) + uint32(b)
					v := uint32(int32(int16(x)))
					if bits == 8 {
						v = uint32(int32(int8(x)))
					}
					cycle += uint64(v)
					if i < bound%period {
						tail += uint64(v)
					}
				}
				contribution = cycle*(bound/period) + tail
			}
			want := uint64(seed + uint32(contribution))
			got := runCompiledAmd64u(t, compiled, bound, uint64(seed))
			compiled.CodeImage.Close()
			if got != want {
				t.Fatalf("bits=%d a=%d b=%d: %x; want %x", bits, a, b, got, want)
			}
		}
	}
}
