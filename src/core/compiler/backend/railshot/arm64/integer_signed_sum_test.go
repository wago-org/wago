//go:build arm64

package arm64

import (
	"math/big"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
			got, err := runArm64Wrapper(t, m, bound, 0xfedcba98)
			if err != nil || got != want.Uint64() {
				t.Fatalf("bits=%d bound=%x: %x, %v; want %x", bits, bound, got, err, want.Uint64())
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
