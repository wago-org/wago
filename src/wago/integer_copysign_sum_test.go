//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func integerCopySignSumModule(wide, negative, compare bool, bits uint64) []byte {
	typ, c, add, lt, fc, convert, neg, copy, reinterpret := wasm.I32, byte(0x41), byte(0x6a), byte(0x49), byte(0x43), byte(0xb3), byte(0x8c), byte(0x98), byte(0xbc)
	raw := make([]byte, 4)
	binary.LittleEndian.PutUint32(raw, uint32(bits))
	if wide {
		typ, c, add, lt, fc, convert, neg, copy, reinterpret = wasm.I64, 0x42, 0x7c, 0x54, 0x44, 0xba, 0x9a, 0xa6, 0xbd
		raw = make([]byte, 8)
		binary.LittleEndian.PutUint64(raw, bits)
	}
	body := []byte{0x20, 1, 0x03, 0x40, 0x20, 1, fc}
	body = append(body, raw...)
	body = append(body, 0x20, 2, c, 1, add, convert)
	if negative {
		body = append(body, neg)
	}
	body = append(body, copy, reinterpret)
	if compare {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(-2147483648)...)
		body = append(body, 0x46)
	}
	body = append(body, add, 0x21, 1, 0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	code := append([]byte{1, 1}, wasm.MustEncodeValType(typ))
	code = append(code, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ, typ}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

func TestIntegerCopySignSumExecution(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, negative := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, abi := range []bool{false, true} {
					bitsList := []uint64{0, 0x80000000, 1, 0x3fc00000, 0x7f800000, 0x7fc12345}
					if wide {
						bitsList = []uint64{0, 0x8000000000000000, 1, 0x3ff8000000000000, 0x7ff0000000000000, 0x7ff8123456789abc}
					}
					for _, bits := range bitsList {
						for _, compare := range []bool{false, true} {
							if compare && wide {
								continue
							}
							t.Run(fmt.Sprintf("wide=%t/negative=%t/opt=%t/abi=%t/bits=%x/compare=%t", wide, negative, enabled, abi, bits, compare), func(t *testing.T) {
								c, err := NewRuntimeConfig().WithOptimization("integer-sum-loop", enabled).WithOptimization("reg-abi", abi).Compile(integerCopySignSumModule(wide, negative, compare, bits))
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
									term := bits & 0x7fffffff
									mask := uint64(1) << 31
									if wide {
										mask = uint64(1) << 63
										term = bits &^ mask
									}
									if negative {
										term |= mask
									}
									if compare {
										if term == 0x80000000 {
											term = 1
										} else {
											term = 0
										}
									}
									for _, n := range bounds {
										count := n
										if count == 0 {
											count = 1
										}
										seed := uint64(91)
										expected := seed + term*count
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
}
