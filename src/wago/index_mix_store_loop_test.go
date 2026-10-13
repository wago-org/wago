//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func indexMixStoreModule(add, mul, mask, offset uint32, first, last int32, boundFirst, nested bool, constant *uint32) []byte {
	b := []byte{0}
	if nested {
		b = append(b, 2, 0x40)
	}
	b = append(b, 3)
	b = append(b, wasmtest.IndexMixStoreLoopBody(add, mul, mask, offset, first, last, boundFirst, constant)...)
	if nested {
		b = append(b, 0x0b)
	}
	b = append(b, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x0b)
	types := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...))))
}

func indexMixStoreOracle(mem []byte, args [4]uint32, add, mul, mask, offset uint32, first, last int32, constant *uint32) (locals [4]uint32, trap bool) {
	locals = args
	bound := args[3]
	if constant != nil {
		bound = *constant
	}
	for iterations := 0; iterations < len(mem)/8+3; iterations++ {
		for _, lane := range []uint32{1, 0} {
			x := locals[1] + lane + add
			x ^= x >> (uint32(first) & 31)
			locals[2] = x * mul
			word := (locals[2] ^ (locals[2] >> (uint32(last) & 31))) & mask
			address := uint64(locals[0]+lane*4) + uint64(offset)
			if address+4 > uint64(len(mem)) {
				return locals, true
			}
			binary.LittleEndian.PutUint32(mem[int(address):], word)
		}
		locals[0] += 8
		locals[1] += 2
		if locals[1] == bound {
			return locals, false
		}
	}
	panic("index-mix oracle did not terminate or trap")
}

func TestIndexMixStorePublicExecution(t *testing.T) {
	frames := map[string][]TrapFrame{}
	for ti, transform := range []struct {
		add, mul, mask uint32
		first, last    int32
	}{{0x9e3779b9, 0x85ebca6b, 65535, 16, 13}, {0xffffffff, 0x80000001, 0xffffffff, 31, -1}, {0, 0, 0, 0, 32}, {17, 31, 0x12345678, 33, 77}} {
		for _, offset := range []uint32{0, 7, 0xffffffff} {
			for _, nested := range []bool{false, true} {
				for _, boundFirst := range []bool{false, true} {
					constant := uint32(10)
					for _, bound := range []*uint32{nil, &constant} {
						for _, on := range []bool{false, true} {
							for _, abi := range []bool{false, true} {
								t.Run(fmt.Sprintf("mix%d/off%d/nested%t/first%t/constant%t/on%t/abi%t", ti, offset, nested, boundFirst, bound != nil, on, abi), func(t *testing.T) {
									cfg := NewRuntimeConfig().WithOptimization("reg-abi", abi)
									if runtime.GOARCH == "arm64" {
										cfg = cfg.WithOptimization("bulk-fill-vector", on)
									}
									c, err := cfg.Compile(indexMixStoreModule(transform.add, transform.mul, transform.mask, offset, transform.first, transform.last, boundFirst, nested, bound))
									if err != nil {
										t.Fatal(err)
									}
									defer c.Close()
									cases := []*Compiled{c}
									if !guardPageBuilt {
										loaded := publicArtifactRoundTrip(t, c)
										defer loaded.Close()
										cases = append(cases, loaded)
									}
									for _, compiled := range cases {
										in, err := Instantiate(compiled)
										if err != nil {
											t.Fatal(err)
										}
										defer in.Close()
										mem := in.Memory().UnsafeBytes()
										before := make([]byte, len(mem))
										for i := range before {
											before[i] = byte(i*37 ^ (i >> 4))
										}
										for ai, args := range [][4]uint32{{0, 0, 99, 2}, {3, 1, 99, 5}, {7, 0, 99, 6}, {16, 0, 99, 10}, {31, 0, 99, 32}, {65528, 0, 99, 2}, {65529, 0, 99, 2}, {65532, 0, 99, 2}, {0xfffffffc, 0, 99, 2}, {0, 0xfffffffe, 99, 0}, {0, 0, 99, 0}, {0, 0, 99, 3}, {0, 10, 99, 2}} {
											copy(mem, before)
											wantMem := bytes.Clone(before)
											want, trap := indexMixStoreOracle(wantMem, args, transform.add, transform.mul, transform.mask, offset, transform.first, transform.last, bound)
											values := make([]uint64, 4)
											for i, v := range args {
												values[i] = uint64(v) | 0xfedcba9800000000
											}
											got, err := in.Invoke("run", values...)
											if trap {
												var tr *TrapError
												if !errors.As(err, &tr) || tr.Code != TrapLinMemOutOfBounds {
													t.Fatalf("args%v expected bounds trap: %v", args, err)
												}
												key := fmt.Sprintf("%d/%d/%t/%t/%t/%t/%d", ti, offset, nested, boundFirst, bound != nil, abi, ai)
												if base, ok := frames[key]; ok {
													if !slices.Equal(base, tr.Frames) {
														t.Fatalf("trap frames changed: %v != %v", tr.Frames, base)
													}
												} else {
													frames[key] = slices.Clone(tr.Frames)
												}
											} else if err != nil || len(got) != 4 || uint32(got[0]) != want[0] || uint32(got[1]) != want[1] || uint32(got[2]) != want[2] || uint32(got[3]) != want[3] {
												t.Fatalf("args%v result%v want%v err%v", args, got, want, err)
											}
											if !bytes.Equal(mem, wantMem) {
												t.Fatalf("args%v memory differs", args)
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
}
