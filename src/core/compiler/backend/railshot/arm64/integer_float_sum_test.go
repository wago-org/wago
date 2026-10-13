//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"math/big"
	"testing"
)

func TestIntegerFloatSumUnsignedExtrema(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, clamp := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 2}
			if clamp {
				body = append(body, 0x41, 1, 0x6a)
			}
			body = append(body, 0xb8, 0x44)
			factor := 3.25
			if clamp {
				factor = 1e10
			}
			body = binary.LittleEndian.AppendUint64(body, math.Float64bits(factor))
			body = append(body, 0xa2, 0xfc)
			if clamp {
				body = append(body, 2)
			} else {
				body = append(body, 6, 0xa7)
			}
			body = append(body, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
			m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
			var stats ModuleStats
			options := CompileOptions{Stats: &stats, Optimizations: map[string]bool{"reg-abi": regABI}}
			c, err := CompileModuleWith(m, options)
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			name := "integer-division-sum-loop"
			if clamp {
				name = "integer-affine-sum-loop"
			}
			if stats.Funcs[0].Peephole[name] != 1 {
				t.Fatalf("clamp=%t regabi=%t reduction not selected", clamp, regABI)
			}
			for _, bound := range []uint64{0, 1, 2, 3, 4, 7, 4096, 1 << 31, (1 << 32) - 1} {
				n := new(big.Int).SetUint64(max(bound, 1))
				last := new(big.Int).Sub(new(big.Int).Set(n), big.NewInt(1))
				want := new(big.Int)
				if clamp {
					want.Mul(n, big.NewInt(0x7fffffff))
				} else {
					// Sum complete quotient levels rather than the emitter's quotient cycles.
					levels := new(big.Int).Quo(new(big.Int).Set(last), big.NewInt(4))
					q := new(big.Int).Mul(levels, n)
					tri := new(big.Int).Mul(levels, new(big.Int).Add(new(big.Int).Set(levels), big.NewInt(1)))
					tri.Rsh(tri, 1)
					tri.Mul(tri, big.NewInt(4))
					q.Sub(q, tri)
					want.Mul(n, last)
					want.Rsh(want, 1)
					want.Mul(want, big.NewInt(3))
					want.Add(want, q)
				}
				want.Add(want, big.NewInt(19))
				got, err := runArm64WrapperWithOptions(t, m, options, bound, 19)
				if err != nil || got != uint64(uint32(want.Uint64())) {
					t.Fatalf("clamp=%t regabi=%t bound=%x got=%x err=%v want=%x", clamp, regABI, bound, got, err, uint32(want.Uint64()))
				}
			}
		}
	}
}
