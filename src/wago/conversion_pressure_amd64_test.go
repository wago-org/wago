//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func conversionPressureModule(params []wasm.ValType, result wasm.ValType, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func compileConversionPressure(t testing.TB, module []byte, pool bool) (*Compiled, *Instance) {
	t.Helper()
	compiled, err := Compile(NewRuntimeConfig().WithOptimization("v128-const-cache", pool), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { compiled.Close() })
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })
	return compiled, instance
}

// Reinterpretation round trips force owned integer registers without changing
// the values. The conversion scratch must spill them on every native path.
func integerConversionPressureModule(n int, source wasm.ValType, conversion []byte) []byte {
	var body []byte
	for j := 0; j < n; j++ {
		body = append(body, 0x20, 0, 0xbf, 0xbd) // get 0; reinterpret f64; reinterpret i64
	}
	params := []wasm.ValType{wasm.I64}
	index := byte(0)
	if source != wasm.I64 {
		params = append(params, source)
		index = 1
	}
	body = append(body, 0x20, index)
	body = append(body, conversion...)
	for j := 0; j < n; j++ {
		body = append(body, 0x7c) // i64.add
	}
	return conversionPressureModule(params, wasm.I64, append(body, 0x0b))
}

func TestUnsignedConversionsPreserveLiveIntegersAMD64(t *testing.T) {
	for _, tc := range []struct {
		name       string
		source     wasm.ValType
		conversion []byte
		saturating bool
	}{
		{"f32.convert_i64_u", wasm.I64, []byte{0xb5, 0xae}, false},
		{"f64.convert_i64_u", wasm.I64, []byte{0xba, 0xb0}, false},
		{"i64.trunc_f32_u", wasm.F32, []byte{0xaf}, false},
		{"i64.trunc_f64_u", wasm.F64, []byte{0xb1}, false},
		{"i64.trunc_sat_f32_u", wasm.F32, []byte{0xfc, 5}, true},
		{"i64.trunc_sat_f64_u", wasm.F64, []byte{0xfc, 7}, true},
	} {
		for _, n := range []int{0, 7} {
			for _, pool := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/live=%d/pool=%v", tc.name, n, pool), func(t *testing.T) {
					_, instance := compileConversionPressure(t, integerConversionPressureModule(n, tc.source, tc.conversion), pool)
					inputs := []float64{3, math.Ldexp(1, 63), 5}
					if tc.saturating {
						inputs = append(inputs, math.NaN(), math.Inf(1), -1)
					}
					for step, input := range inputs {
						live := uint64(7 + step*4)
						args := []uint64{live}
						converted := live
						if tc.source != wasm.I64 {
							bits := F64(input)
							if tc.source == wasm.F32 {
								bits = F32(float32(input))
							}
							args = append(args, bits)
							switch {
							case math.IsNaN(input) || input < 0:
								converted = 0
							case math.IsInf(input, 1):
								converted = math.MaxUint64
							default:
								converted = uint64(input)
							}
						}
						got, err := instance.Invoke("run", args...)
						want := uint64(n)*live + converted
						if err != nil || len(got) != 1 || got[0] != want {
							t.Errorf("live=%d input=%g: got %v, %v; want %d", live, input, got, err, want)
						}
					}
				})
			}
		}
	}
}

func signedSaturationPressureModule(n int, source wasm.ValType, wide bool) []byte {
	var body []byte
	for j := 0; j < n; j++ {
		body = append(body, 0x20, 0, 0x20, 0, 0xa0) // eager f64.add keeps x+x live in XMM
	}
	subop, convert := byte(0), byte(0xb7) // i32.trunc_sat_f32_s; f64.convert_i32_s
	if source == wasm.F64 {
		subop += 2
	}
	if wide {
		subop += 4
		convert = 0xb9
	}
	body = append(body, 0x20, 1, 0xfc, subop, convert)
	for j := 0; j < n; j++ {
		body = append(body, 0xa0)
	}
	return conversionPressureModule([]wasm.ValType{wasm.F64, source}, wasm.F64, append(body, 0x0b))
}

func TestSignedSaturationPreservesLiveFloatsAMD64(t *testing.T) {
	for _, source := range []wasm.ValType{wasm.F32, wasm.F64} {
		for _, wide := range []bool{false, true} {
			for _, n := range []int{0, 12, 13} {
				for _, pool := range []bool{false, true} {
					t.Run(fmt.Sprintf("source=%v/i64=%v/live=%d/pool=%v", source, wide, n, pool), func(t *testing.T) {
						_, instance := compileConversionPressure(t, signedSaturationPressureModule(n, source, wide), pool)
						// Warm the same frame on the ordered path, then change the live values.
						// Repeating identical values could hide a skipped spill behind stale data.
						for _, args := range [][2]float64{{3, 0}, {9, math.NaN()}, {5, math.NaN()}, {7, 3}} {
							input := F64(args[1])
							if source == wasm.F32 {
								input = F32(float32(args[1]))
							}
							got, err := instance.Invoke("run", F64(args[0]), input)
							want := float64(n*2) * args[0]
							if !math.IsNaN(args[1]) {
								want += args[1]
							}
							if err != nil || len(got) != 1 || got[0] != F64(want) {
								t.Errorf("input=%g/%g: got %v, %v; want %g", args[0], args[1], got, err, want)
							}
						}
					})
				}
			}
		}
	}
}
