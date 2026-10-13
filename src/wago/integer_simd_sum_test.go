//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerSIMDSumModule(wide bool, lane byte, a, b uint64) []byte {
	typ, c, add, lt, splat, extract := wasm.I32, byte(0x41), byte(0x6a), byte(0x49), byte(17), byte(27)
	vectorAdd, vectorMul := uint32(174), uint32(181)
	if wide {
		typ, c, add, lt, splat, extract = wasm.I64, 0x42, 0x7c, 0x54, 18, 29
		vectorAdd, vectorMul = 206, 213
	}
	constant := func(v uint64) []byte {
		if wide {
			return append([]byte{c}, wasmtest.SLEB64(int64(v))...)
		}
		return append([]byte{c}, wasmtest.SLEB32(int32(v))...)
	}
	body := []byte{0x20, 1, 0x03, 0x40, 0x20, 1, 0x20, 2, 0xfd, splat}
	body = append(body, constant(a)...)
	body = append(body, 0xfd, splat, 0xfd)
	body = append(body, wasmtest.ULEB(vectorMul)...)
	body = append(body, constant(b)...)
	body = append(body, 0xfd, splat, 0xfd)
	body = append(body, wasmtest.ULEB(vectorAdd)...)
	body = append(body, 0xfd, extract, lane)
	body = append(body, add, 0x21, 1, 0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1}, wasm.MustEncodeValType(typ))
	code = append(code, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestIntegerSIMDSumExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		lanes := byte(4)
		if wide {
			lanes = 2
		}
		for lane := byte(0); lane < lanes; lane++ {
			for _, enabled := range []bool{false, true} {
				for _, abi := range []bool{false, true} {
					for _, coefficients := range [][2]uint64{{7, 21}, {^uint64(0), 91}, {0, 0x8000000000000001}} {
						a, b := coefficients[0], coefficients[1]
						t.Run(fmt.Sprintf("wide=%t/lane=%d/opt=%t/abi=%t/a=%x/b=%x", wide, lane, enabled, abi, a, b), func(t *testing.T) {
							c, err := NewRuntimeConfig().WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", abi).Compile(integerSIMDSumModule(wide, lane, a, b))
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
								bounds := []uint64{0, 1, 2, 63, 4096}
								if enabled {
									if wide {
										bounds = append(bounds, 0x8000000000000000, ^uint64(0))
									} else {
										bounds = append(bounds, 0x80000000, 0xffffffff)
									}
								}
								for _, n := range bounds {
									count := n
									if count == 0 {
										count = 1
									}
									x, y := count, count-1
									if x&1 == 0 {
										x /= 2
									} else {
										y /= 2
									}
									seed := uint64(91)
									expected := seed + a*x*y + b*count
									if !wide {
										expected = uint64(uint32(expected))
									}
									got, err := in.Invoke("sum", n, seed)
									if err != nil {
										t.Fatal(err)
									}
									if len(got) != 3 || got[0] != seed || got[1] != expected || got[2] != count {
										t.Fatalf("n=%x got=%x expected=%x counter=%x", n, got, expected, count)
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
