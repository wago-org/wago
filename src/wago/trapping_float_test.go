//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

func trappingFloatModule(sourceWide, resultWide, signed bool) []byte {
	source, result := wasm.F32, wasm.I32
	subop := byte(0xa8)
	if sourceWide {
		source = wasm.F64
		subop += 2
	}
	if resultWide {
		result = wasm.I64
		subop += 6
	}
	if !signed {
		subop++
	}
	body := []byte{0, 0x20, 0, subop, 0x20, 0, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{source}, []wasm.ValType{result, source}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestTrappingFloatPublicExecution(t *testing.T) {
	for _, sourceWide := range []bool{false, true} {
		for _, resultWide := range []bool{false, true} {
			for _, signed := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("source64=%t/result64=%t/signed=%t/regabi=%t", sourceWide, resultWide, signed, regABI), func(t *testing.T) {
						module := trappingFloatModule(sourceWide, resultWide, signed)
						c, err := NewRuntimeConfig().WithOptimization("reg-abi", regABI).Compile(module)
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							inputs := []float64{0, math.Copysign(0, -1), 1, -1, 0.75, -0.75, -2147483648.75, -2147483649, math.Inf(1), math.Inf(-1), math.NaN()}
							for _, limit := range []float64{math.Ldexp(1, 31), math.Ldexp(1, 32), math.Ldexp(1, 63), math.Ldexp(1, 64)} {
								for _, value := range []float64{limit, -limit} {
									inputs = append(inputs, value, math.Nextafter(value, math.Inf(1)), math.Nextafter(value, math.Inf(-1)))
								}
							}
							bits := []uint64{}
							for _, x := range inputs {
								if sourceWide {
									bits = append(bits, math.Float64bits(x))
								} else {
									v := float32(x)
									bits = append(bits, uint64(math.Float32bits(v)), uint64(math.Float32bits(math.Nextafter32(v, float32(math.Inf(1))))), uint64(math.Float32bits(math.Nextafter32(v, float32(math.Inf(-1))))))
								}
							}
							if sourceWide {
								bits = append(bits, 1, 0x8000000000000001, 0x7ff0000000000001, 0xfff0000000000001, 0x7ff8123456789abc, 0xfff8123456789abc)
							} else {
								bits = append(bits, 1, 0x80000001, 0x7f800001, 0xff800001, 0x7fc12345, 0xffc12345)
							}
							bits = append(bits, 0)
							n := 32
							if resultWide {
								n = 64
							}
							low, hi := 0.0, math.Ldexp(1, n)
							if signed {
								hi = math.Ldexp(1, n-1)
								low = -hi
							}
							for _, raw := range bits {
								x := math.Float64frombits(raw)
								if !sourceWide {
									x = float64(math.Float32frombits(uint32(raw)))
								}
								value := math.Trunc(x)
								valid := !math.IsNaN(x) && value >= low && value < hi
								got, err := in.Invoke("convert", raw)
								if !valid {
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != TrapTruncOverflow {
										t.Fatalf("bits=%x got=%v err=%v want trunc overflow", raw, got, err)
									}
									continue
								}
								want := saturatingFloatOracle(x, resultWide, signed)
								if err != nil || len(got) != 2 || got[0] != want || got[1] != raw {
									t.Fatalf("bits=%x got=%v err=%v want=%x/live=%x", raw, got, err, want, raw)
								}
							}
						}
					})
				}
			}
		}
	}
}

func trappingFloatGlobalModule(sourceWide, resultWide, signed bool) []byte {
	source, constant, reinterpret, op := wasm.F32, byte(0x43), byte(0xbc), byte(0xa8)
	if sourceWide {
		source = wasm.F64
		constant = 0x44
		reinterpret = 0xbd
		op += 2
	}
	if resultWide {
		op += 6
	}
	if !signed {
		op++
	}
	rawType := wasm.I32
	zero := make([]byte, 4)
	if sourceWide {
		rawType = wasm.I64
		zero = make([]byte, 8)
	}
	global := append([]byte{0x7d, 1, constant}, zero...)
	// F64's value type is 0x7c, one below F32.
	global[0] = 0x7d
	if sourceWide {
		global[0] = 0x7c
	}
	global = append(global, 0x0b)
	body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 0, 0x24, 0, 0x23, 0, 0x1a, 0x23, 0, op, 0x1a, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 1, 0x49, 0x0d, 0, 0x0b, 0x0b}
	getter := []byte{0, 0x23, 0, reinterpret, 0x0b}
	code := func(b []byte) []byte { return append(wasmtest.ULEB(uint32(len(b))), b...) }
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{source, wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{rawType}))), wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})), wasmtest.Section(6, wasmtest.Vec(global)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0), wasmtest.ExportEntry("state", 0, 1))), wasmtest.Section(10, wasmtest.Vec(code(body), code(getter))))
}
func TestTrappingFloatPublishesDirtyGlobalOnOverflow(t *testing.T) {
	for _, sourceWide := range []bool{false, true} {
		for _, resultWide := range []bool{false, true} {
			for _, signed := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					c, err := NewRuntimeConfig().WithOptimization("reg-abi", regABI).Compile(trappingFloatGlobalModule(sourceWide, resultWide, signed))
					if err != nil {
						t.Fatal(err)
					}
					for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
						in, err := Instantiate(compiled)
						if err != nil {
							t.Fatal(err)
						}
						rawNaN := uint64(0x7f812345)
						if sourceWide {
							rawNaN = 0x7ff0123456789abc
						}
						for _, bits := range []uint64{rawNaN, 0, rawNaN, 0} {
							_, err := in.Invoke("convert", bits, 2)
							if bits == rawNaN {
								var trap *TrapError
								if !errors.As(err, &trap) || trap.Code != TrapTruncOverflow {
									t.Fatalf("expected trunc trap, got %v", err)
								}
							} else if err != nil {
								t.Fatal(err)
							}
							got, err := in.Invoke("state")
							if err != nil || len(got) != 1 || got[0] != bits {
								t.Fatalf("source64=%t/result64=%t/signed=%t/regabi=%t state=%v err=%v want=%x", sourceWide, resultWide, signed, regABI, got, err, bits)
							}
						}
						in.Close()
					}
					c.Close()
				}
			}
		}
	}
}
