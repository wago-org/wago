//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"runtime"
	"testing"
)

func booleanFloatModule(wide, signed bool, a, b int32) []byte {
	floatType, conversion, reinterpret, resultType := byte(0x7d), byte(0xb3), byte(0xbc), wasm.I32
	if signed {
		conversion = 0xb2
	}
	if wide {
		floatType, reinterpret, resultType = 0x7c, 0xbd, wasm.I64
		conversion = 0xb8
		if signed {
			conversion = 0xb7
		}
	}
	body := []byte{2, 1, 0x7f, 1, floatType, 0x03, 0x40, 0x41}
	body = append(body, wasmtest.SLEB32(b)...)
	body = append(body, 0x20, 0, 0x41, 1, 0x71, 0x41)
	body = append(body, wasmtest.SLEB32(a)...)
	body = append(body, 0x6c, 0x6a, conversion, 0x21, 3, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 1, 0x49, 0x0d, 0, 0x0b, 0x20, 3, reinterpret, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{resultType}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestBooleanFloatPublicExecution(t *testing.T) {
	for _, regABI := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, signed := range []bool{false, true} {
					for _, coefficients := range [][2]int32{{2, 1}, {1, 0}, {-2147483648, 0}, {-1, 2147483647}, {97, -19}} {
						t.Run(fmt.Sprintf("regabi=%t/enabled=%t/wide=%t/signed=%t/coeff=%v", regABI, enabled, wide, signed, coefficients), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithOptimization("reg-abi", regABI)
							if runtime.GOARCH == "arm64" {
								cfg = cfg.WithOptimization("boolean-float-select", enabled)
							}
							c, err := cfg.Compile(booleanFloatModule(wide, signed, coefficients[0], coefficients[1]))
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
								for _, x := range []uint32{0, 1, 2, 3, 255, 0x80000001, 0xffffffff} {
									for _, bound := range []uint32{0, 1, 2, 17} {
										integer := uint32(coefficients[1]) + (x&1)*uint32(coefficients[0])
										number := float64(integer)
										if signed {
											number = float64(int32(integer))
										}
										want := math.Float64bits(number)
										if !wide {
											want = uint64(math.Float32bits(float32(number)))
										}
										got, err := in.Invoke("convert", uint64(x), uint64(bound))
										if err != nil || len(got) != 1 || got[0] != want {
											t.Fatalf("x=%x bound=%d got=%v err=%v want=%x", x, bound, got, err, want)
										}
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
