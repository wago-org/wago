//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/bits"
	"testing"
)

func integerPopcountSumModule(wide bool, rotations []int64) []byte {
	typ, constant, add, less, pop, rotate := wasm.I32, byte(0x41), byte(0x6a), byte(0x49), byte(0x69), byte(0x77)
	if wide {
		typ, constant, add, less, pop, rotate = wasm.I64, 0x42, 0x7c, 0x54, 0x7b, 0x89
	}
	localType := byte(0x7f)
	if wide {
		localType = 0x7e
	}
	body := []byte{0x20, 1, 0x03, 0x40, 0x20, 1, 0x20, 2}
	for i, rotation := range rotations {
		body = append(body, constant)
		if wide {
			body = append(body, wasmtest.SLEB64(rotation)...)
		} else {
			body = append(body, wasmtest.SLEB32(int32(rotation))...)
		}
		body = append(body, rotate+byte(i%2))
	}
	body = append(body, pop, add, 0x21, 1, 0x20, 2, constant, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, less, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1, localType}, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestIntegerPopcountSumPublicExecution(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, rotations := range [][]int64{nil, {7}, {-7, 64}, {0, -1, 33, 127}} {
					t.Run(fmt.Sprintf("enabled=%t/regabi=%t/wide=%t/rotations=%v", enabled, regABI, wide, rotations), func(t *testing.T) {
						c, err := NewRuntimeConfig().WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", regABI).Compile(integerPopcountSumModule(wide, rotations))
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
							for _, bound := range []uint64{0, 1, 2, 3, 15, 31, 32, 63, 64, 127, 128, 255, 256, 257, 4096, 65535} {
								seed := uint64(0xfedcba9876543210)
								if !wide {
									seed = uint64(uint32(seed))
								}
								want := seed
								counter := uint64(0)
								for {
									v := counter
									for i, r := range rotations {
										if i%2 != 0 {
											r = -r
										}
										if wide {
											v = bits.RotateLeft64(v, int(r))
										} else {
											v = uint64(bits.RotateLeft32(uint32(v), int(r)))
										}
									}
									if wide {
										want += uint64(bits.OnesCount64(v))
									} else {
										want = uint64(uint32(want) + uint32(bits.OnesCount32(uint32(v))))
									}
									counter++
									if counter >= bound {
										break
									}
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
