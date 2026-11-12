//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"math/rand"
	"testing"
)

func saturatingFloatModule(sourceWide, resultWide, signed bool) []byte {
	source, result := wasm.F32, wasm.I32
	subop := byte(0)
	if sourceWide {
		source = wasm.F64
		subop += 2
	}
	if resultWide {
		result = wasm.I64
		subop += 4
	}
	if !signed {
		subop++
	}
	body := []byte{0, 0x20, 0, 0xfc, subop, 0x20, 0, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{source}, []wasm.ValType{result, source}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func saturatingFloatOracle(x float64, wide, signed bool) uint64 {
	if math.IsNaN(x) {
		return 0
	}
	n := 32
	if wide {
		n = 64
	}
	if signed {
		limit := math.Ldexp(1, n-1)
		if x >= limit {
			if wide {
				return 0x7fffffffffffffff
			}
			return 0x7fffffff
		}
		if x <= -limit {
			if wide {
				return 0x8000000000000000
			}
			return 0x80000000
		}
		if wide {
			return uint64(int64(x))
		}
		return uint64(uint32(int32(x)))
	}
	if x <= 0 {
		return 0
	}
	if x >= math.Ldexp(1, n) {
		if wide {
			return 0xffffffffffffffff
		}
		return 0xffffffff
	}
	return uint64(x)
}

func TestSaturatingFloatPublicExecution(t *testing.T) {
	for _, sourceWide := range []bool{false, true} {
		inputs := []uint64{}
		for _, x := range []float64{0, math.Copysign(0, -1), 1, -1, 0.75, -0.75, 2147483647, -2147483648, 4294967295, math.Ldexp(1, 63), -math.Ldexp(1, 63), math.Ldexp(1, 64), math.Inf(1), math.Inf(-1), math.NaN()} {
			for _, y := range []float64{x, math.Nextafter(x, math.Inf(1)), math.Nextafter(x, math.Inf(-1))} {
				if sourceWide {
					inputs = append(inputs, math.Float64bits(y))
				} else {
					z := float32(y)
					inputs = append(inputs, uint64(math.Float32bits(z)), uint64(math.Float32bits(math.Nextafter32(z, float32(math.Inf(1))))), uint64(math.Float32bits(math.Nextafter32(z, float32(math.Inf(-1))))))
				}
			}
		}
		if sourceWide {
			inputs = append(inputs, 1, 0x8000000000000001, 0x7ff0000000000001, 0xfff0000000000001, 0x7ff8123456789abc, 0xfff8123456789abc)
		} else {
			inputs = append(inputs, 1, 0x80000001, 0x7f800001, 0xff800001, 0x7fc12345, 0xffc12345)
		}
		rng := rand.New(rand.NewSource(4096))
		for range 1000 {
			v := rng.Uint64()
			if !sourceWide {
				v = uint64(uint32(v))
			}
			inputs = append(inputs, v)
		}
		for _, resultWide := range []bool{false, true} {
			for _, signed := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("source64=%t/result64=%t/signed=%t/regabi=%t", sourceWide, resultWide, signed, regABI), func(t *testing.T) {
						c, err := NewRuntimeConfig().WithOptimization("reg-abi", regABI).Compile(saturatingFloatModule(sourceWide, resultWide, signed))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						for _, compiled := range []*Compiled{c} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							for _, bits := range inputs {
								x := math.Float64frombits(bits)
								if !sourceWide {
									x = float64(math.Float32frombits(uint32(bits)))
								}
								want := saturatingFloatOracle(x, resultWide, signed)
								got, err := in.Invoke("convert", bits)
								if err != nil || len(got) != 2 || got[0] != want || got[1] != bits {
									t.Fatalf("bits=%x got=%v err=%v want=%x", bits, got, err, want)
								}
							}
						}
					})
				}
			}
		}
	}
}
