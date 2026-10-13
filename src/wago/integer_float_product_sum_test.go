//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

func integerFloatProductSumModule(wide, clamp, unsigned bool, factor float64) []byte {
	body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 2}
	if clamp {
		body = append(body, 0x41, 1, 0x6a)
	}
	convert, constant, multiply, sub := byte(0xb3), byte(0x43), byte(0x94), byte(0)
	if wide {
		convert, constant, multiply, sub = 0xb8, 0x44, 0xa2, 2
	}
	if !clamp {
		sub = 6
	}
	if unsigned {
		sub++
	}
	body = append(body, convert, constant)
	if wide {
		body = binary.LittleEndian.AppendUint64(body, math.Float64bits(factor))
	} else {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(float32(factor)))
	}
	body = append(body, multiply, 0xfc, sub)
	if !clamp {
		body = append(body, 0xa7)
	}
	body = append(body, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}
func TestIntegerFloatSumPublicExecution(t *testing.T) {
	for _, c := range []struct {
		wide, clamp, unsigned bool
		factor                float64
	}{{true, false, false, 3.25}, {true, false, true, 0.125}, {true, false, false, 1.5}, {true, false, false, 4}, {true, false, false, 2097152}, {true, false, false, 2147483648}, {true, false, true, 4294967296}, {false, true, false, math.Ldexp(1, 31)}, {true, true, false, 1e10}, {false, true, true, math.Ldexp(1, 32)}, {false, true, false, 1.5}, {true, false, false, 3.75}, {true, false, false, 2097153}} {
		for _, enabled := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("case=%+v/enabled=%t/regabi=%t", c, enabled, regABI), func(t *testing.T) {
					compiled, err := NewRuntimeConfig().WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", regABI).Compile(integerFloatProductSumModule(c.wide, c.clamp, c.unsigned, c.factor))
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					for _, artifact := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
						in, err := Instantiate(artifact)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						for _, n := range []uint32{0, 1, 2, 7, 31, 1024, 4096} {
							for _, seed := range []uint32{0, 19, 0xffffffff} {
								want := seed
								for i := uint32(0); i < max(n, 1); i++ {
									x := i
									if c.clamp {
										x++
									}
									v := float64(x) * c.factor
									if !c.wide {
										v = float64(float32(x) * float32(c.factor))
									}
									want += uint32(saturatingFloatOracle(v, !c.clamp, !c.unsigned))
								}
								got, err := in.Invoke("sum", uint64(n), uint64(seed))
								if err != nil || len(got) != 2 || got[0] != uint64(want) || got[1] != uint64(max(n, 1)) {
									t.Fatalf("n=%d seed=%x got=%v err=%v want=%x counter=%d", n, seed, got, err, want, max(n, 1))
								}
							}
						}
					}
				})
			}
		}
	}
}
