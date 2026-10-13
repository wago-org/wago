//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerDivisionSumModule(wide bool, divisor uint64, coefficients [4]int64) []byte {
	typ, constant, add, mul, div, rem, less := wasm.I32, byte(0x41), byte(0x6a), byte(0x6c), byte(0x6e), byte(0x70), byte(0x49)
	localType := byte(0x7f)
	if wide {
		typ, constant, add, mul, div, rem, less = wasm.I64, 0x42, 0x7c, 0x7e, 0x80, 0x82, 0x54
		localType = 0x7e
	}
	body := []byte{0x20, 1, 0x03, 0x40, 0x20, 1, 0x20, 2}
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
	c(coefficients[2])
	body = append(body, mul, 0x20, 2)
	c(int64(divisor))
	body = append(body, rem)
	c(coefficients[3])
	body = append(body, mul, add, 0x20, 2)
	c(coefficients[0])
	body = append(body, mul, add)
	c(coefficients[1])
	body = append(body, add)
	body = append(body, add, 0x21, 1, 0x20, 2, constant, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, less, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1, localType}, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestIntegerDivisionSumPublicExecution(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				divisors := []uint64{1, 2, 7, 65537, (1 << 31) + 1, (1 << 32) - 1}
				if wide {
					divisors = append(divisors, 1<<63, ^uint64(0))
				}
				for _, divisor := range divisors {
					for _, coefficients := range [][4]int64{{0, 0, 1, 0}, {0, 0, 0, 1}, {13, -19, -3, 5}} {
						t.Run(fmt.Sprintf("enabled=%t/regabi=%t/wide=%t/divisor=%x/coefficients=%v", enabled, regABI, wide, divisor, coefficients), func(t *testing.T) {
							c, err := NewRuntimeConfig().WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", regABI).Compile(integerDivisionSumModule(wide, divisor, coefficients))
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
								for _, bound := range []uint64{0, 1, 2, 3, 6, 7, 8, 15, 255, 256, 257, 4096, 16383} {
									seed := uint64(0xfedcba9876543210)
									if !wide {
										seed = uint64(uint32(seed))
									}
									want, counter := seed, uint64(0)
									for {
										want += counter*uint64(coefficients[0]) + uint64(coefficients[1]) + (counter/divisor)*uint64(coefficients[2]) + (counter%divisor)*uint64(coefficients[3])
										counter++
										if counter >= bound {
											break
										}
									}
									if !wide {
										want = uint64(uint32(want))
									}
									got, err := in.Invoke("sum", bound, seed)
									if err != nil || len(got) != 3 || got[0] != seed || got[1] != want || got[2] != counter {
										t.Fatalf("bound=%d got=%v, %v; want [%d %d %d]", bound, got, err, seed, want, counter)
									}
								}
							}
						})
					}
				}
			}
		}
	}
}
