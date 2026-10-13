//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerSignedSumModule(bits uint8, bridge, negative bool, a, b int32) []byte {
	body := []byte{0x20, 1, 0x03, 0x40, 0x20, 1, 0x20, 2, 0x41}
	body = append(body, wasmtest.SLEB32(a)...)
	body = append(body, 0x6c, 0x41)
	body = append(body, wasmtest.SLEB32(b)...)
	body = append(body, 0x6a)
	if bridge {
		body = append(body, 0xad)
		code := byte(0xc2)
		if bits == 16 {
			code = 0xc3
		} else if bits == 32 {
			code = 0xc4
		}
		body = append(body, code)
		if negative {
			body = append(body, 0x42, 32, 0x88)
		}
		body = append(body, 0xa7)
	} else if bits == 8 {
		body = append(body, 0xc0)
	} else {
		body = append(body, 0xc1)
	}
	body = append(body, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1, 0x7f}, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestIntegerSignedSumPublicExecution(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			for _, shape := range []struct {
				bits             uint8
				bridge, negative bool
			}{{8, false, false}, {16, false, false}, {8, true, false}, {16, true, false}, {32, true, false}, {8, true, true}, {16, true, true}, {32, true, true}} {
				for _, coefficients := range [][2]int32{{13, 0}, {97, -19}, {-2147483647, 2147483647}, {0, -1}} {
					t.Run(fmt.Sprintf("enabled=%t/regabi=%t/shape=%v/coefficients=%v", enabled, regABI, shape, coefficients), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", regABI)
						c, err := cfg.Compile(integerSignedSumModule(shape.bits, shape.bridge, shape.negative, coefficients[0], coefficients[1]))
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
							for _, bound := range []uint32{0, 1, 2, 15, 128, 255, 256, 257, 4096, 65535} {
								seed := uint32(0xfedcba98)
								want, counter := seed, uint32(0)
								for {
									x := uint64(counter*uint32(coefficients[0]) + uint32(coefficients[1]))
									mask := uint64(1)<<shape.bits - 1
									x &= mask
									var value uint32
									if shape.negative {
										if x&(uint64(1)<<(shape.bits-1)) != 0 {
											value = ^uint32(0)
										}
									} else {
										if x&(uint64(1)<<(shape.bits-1)) != 0 {
											x |= ^mask
										}
										value = uint32(x)
									}
									want += value
									counter++
									if counter >= bound {
										break
									}
								}
								got, err := in.Invoke("sum", uint64(bound), uint64(seed))
								if err != nil || len(got) != 3 || got[0] != uint64(seed) || got[1] != uint64(want) || got[2] != uint64(counter) {
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
