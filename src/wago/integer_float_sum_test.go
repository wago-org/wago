//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerFloatSumModule(wide, f64 bool) []byte {
	typ, c, add, lt, convert, sat := wasm.I32, byte(0x41), byte(0x6a), byte(0x49), byte(0xb3), byte(1)
	neg := byte(0x8c)
	if wide {
		typ, c, add, lt, convert, sat = wasm.I64, 0x42, 0x7c, 0x54, 0xb5, 5
	}
	if f64 {
		convert += 5
		neg = 0x9a
		sat += 2
	}
	// Preserve the seed below the loop and return both the sum and final counter.
	body := []byte{0x20, 1, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x20, 2, convert, neg, 0xfc, sat, add, add, 0x21, 1,
		0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b}
	code := append([]byte{1, 1}, wasm.MustEncodeValType(typ))
	code = append(code, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestIntegerFloatSignSumExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, f64 := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, abi := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%t/f64=%t/opt=%t/abi=%t", wide, f64, enabled, abi), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", abi)
						c, err := cfg.Compile(integerFloatSumModule(wide, f64))
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
							bounds := []uint64{0, 1, 2, 3, 63, 4096}
							if enabled {
								if wide {
									bounds = append(bounds, 0x8000000000000000, ^uint64(0))
								} else {
									bounds = append(bounds, 0x80000000, 0xffffffff)
								}
							}
							for _, n := range bounds {
								seed := uint64(0xfffffff1)
								got, err := in.Invoke("sum", n, seed)
								if err != nil {
									t.Fatal(err)
								}
								// Divide one factor before multiplying to preserve the modular sum.
								count := n
								if count == 0 {
									count = 1
								}
								a, b := count, count-1
								if a&1 == 0 {
									a /= 2
								} else {
									b /= 2
								}
								expected := seed + a*b
								if !wide {
									expected = uint64(uint32(expected))
								}
								if len(got) != 3 || got[0] != seed || got[1] != expected || got[2] != count {
									t.Fatalf("n=%x: got=%x expected sum=%x counter=%x", n, got, expected, count)
								}
							}
						}
					})
				}
			}
		}
	}
}
