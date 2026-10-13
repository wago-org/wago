//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/big"
	"testing"
)

func TestIntegerDivisionSumUnsignedExtrema(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		typ, constant, add, mul, div, rem, less := wasm.I32, byte(0x41), byte(0x6a), byte(0x6c), byte(0x6e), byte(0x70), byte(0x49)
		localType, width := byte(0x7f), uint(32)
		divisors := []uint64{1, 2, 7, 65537, (1 << 31) + 1, (1 << 32) - 1}
		bounds := []uint64{0, 1, 2, 3, 6, 7, 8, 65535, 65536, 1 << 31, (1 << 31) + 1, (1 << 32) - 1}
		if wide {
			typ, constant, add, mul, div, rem, less = wasm.I64, 0x42, 0x7c, 0x7e, 0x80, 0x82, 0x54
			localType, width = 0x7e, 64
			divisors = append(divisors, 1<<63, ^uint64(0))
			bounds = append(bounds, 1<<32, 1<<63, (1<<63)+1, ^uint64(0))
		}
		for _, divisor := range divisors {
			body := []byte{1, 1, localType, 0x03, 0x40, 0x20, 1, 0x20, 2}
			c := func(v int64) {
				body = append(body, constant)
				if wide {
					body = append(body, wasmtest.SLEB64(v)...)
				} else {
					body = append(body, wasmtest.SLEB32(int32(v))...)
				}
			}
			c(int64(divisor))
			body = append(body, div)
			c(-3)
			body = append(body, mul, 0x20, 2)
			c(int64(divisor))
			body = append(body, rem)
			c(5)
			body = append(body, mul, add, 0x20, 2)
			c(13)
			body = append(body, mul, add)
			c(-19)
			body = append(body, add)
			body = append(body, add, 0x21, 1, 0x20, 2, constant, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, less, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
			m := modFuncs(t, funcDef{params: []wasm.ValType{typ, typ}, results: []wasm.ValType{typ}, body: body})
			var stats ModuleStats
			compiled, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			compiled.CodeImage.Close()
			if stats.Funcs[0].Peephole["integer-division-sum-loop"] != 1 {
				t.Fatalf("wide=%t divisor=%x: reduction not selected", wide, divisor)
			}
			for _, bound := range bounds {
				n := new(big.Int).SetUint64(max(bound, 1))
				d := new(big.Int).SetUint64(divisor)
				// Count the values >=j*D for every nonzero quotient level j. This
				// complementary arbitrary-precision sum independently checks the emitted
				// complete-quotient-cycles/remainder formula and all intermediate wrap.
				last := new(big.Int).Sub(new(big.Int).Set(n), big.NewInt(1))
				levels := new(big.Int).Quo(last, d)
				qsum := new(big.Int).Mul(levels, n)
				progression := new(big.Int).Mul(levels, new(big.Int).Add(new(big.Int).Set(levels), big.NewInt(1)))
				progression.Quo(progression, big.NewInt(2))
				progression.Mul(progression, d)
				qsum.Sub(qsum, progression)
				triangle := new(big.Int).Mul(n, last)
				triangle.Quo(triangle, big.NewInt(2))
				rsum := new(big.Int).Sub(new(big.Int).Set(triangle), new(big.Int).Mul(d, qsum))
				want := new(big.Int).Mul(triangle, big.NewInt(13))
				want.Add(want, new(big.Int).Mul(n, big.NewInt(-19)))
				want.Add(want, new(big.Int).Mul(qsum, big.NewInt(-3)))
				want.Add(want, new(big.Int).Mul(rsum, big.NewInt(5)))
				want.Add(want, new(big.Int).SetUint64(0xfedcba9876543210))
				want.Mod(want, new(big.Int).Lsh(big.NewInt(1), width))
				got, err := runArm64Wrapper(t, m, bound, 0xfedcba9876543210)
				if err != nil {
					t.Fatal(err)
				}
				if got != want.Uint64() {
					t.Fatalf("wide=%t divisor=%x bound=%x: %x want %x", wide, divisor, bound, got, want.Uint64())
				}
			}
			for name, opt := range map[string]CompileOptions{"disabled": {Optimizations: map[string]bool{"integer-sum-loop": false}}, "interruptible": {Interruptible: true}} {
				var stats ModuleStats
				opt.Stats = &stats
				compiled, err := CompileModuleWith(m, opt)
				if err != nil {
					t.Fatal(err)
				}
				compiled.CodeImage.Close()
				if stats.Funcs[0].Peephole["integer-division-sum-loop"] != 0 {
					t.Fatalf("%s selected reduction", name)
				}
			}
		}
	}
}
