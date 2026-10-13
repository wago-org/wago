//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"slices"
	"testing"
)

func integerHashStoreModule(x, m, mask, bound, offset uint32, shift int32, nested bool) []byte {
	constant := func(v uint32) []byte { return append([]byte{0x41}, wasmtest.SLEB64(int64(int32(v)))...) }
	expression := func(second bool) []byte {
		b := []byte{0x20, 1}
		if second {
			b = append(b, 0x41, 1, 0x6a)
		}
		b = append(b, constant(x)...)
		b = append(b, 0x73)
		b = append(b, constant(m)...)
		b = append(b, 0x6c, 0x22, 2)
		b = append(b, constant(uint32(shift))...)
		b = append(b, 0x76, 0x20, 2, 0x73)
		b = append(b, constant(mask)...)
		return append(b, 0x71)
	}
	store := append([]byte{0x36, 2}, wasmtest.ULEB(offset)...)
	b := []byte{1, 3, 0x7f}
	if nested {
		b = append(b, 0x20, 1, 0x21, 3, 0x03, 0x40, 0x20, 3, 0x21, 1)
	}
	b = append(b, 0x03, 0x40, 0x20, 0)
	b = append(b, expression(false)...)
	b = append(b, store...)
	b = append(b, 0x20, 0, 0x41, 4, 0x6a)
	b = append(b, expression(true)...)
	b = append(b, store...)
	b = append(b, 0x20, 0, 0x41, 8, 0x6a, 0x21, 0, 0x20, 1, 0x41, 2, 0x6a, 0x22, 1)
	b = append(b, constant(bound)...)
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	if nested {
		b = append(b, 0x20, 4, 0x41, 1, 0x6a, 0x22, 4, 0x41, 3, 0x49, 0x0d, 0, 0x0b)
	}
	b = append(b, 0x20, 0, 0x20, 1, 0x20, 2, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...))))
}

func integerHashStoreOracle(mem []byte, dst, seed, x, m, mask, bound, offset uint32, shift int32, nested bool) ([]uint64, bool) {
	reps := 1
	if nested {
		reps = 3
	}
	originalSeed := seed
	var temporary uint32
	for rep := 0; rep < reps; rep++ {
		if nested {
			seed = originalSeed
		}
		for iterations := 0; iterations < 10000; iterations++ {
			for lane := uint32(0); lane < 2; lane++ {
				temporary = ((seed + lane) ^ x) * m
				word := (temporary ^ (temporary >> (uint32(shift) & 31))) & mask
				address := uint64(dst+lane*4) + uint64(offset)
				if address+4 > uint64(len(mem)) {
					return nil, true
				}
				binary.LittleEndian.PutUint32(mem[int(address):], word)
			}
			dst += 8
			seed += 2
			if seed == bound {
				break
			}
			if iterations == 9999 {
				panic("hash-store oracle did not terminate or trap")
			}
		}
	}
	return []uint64{uint64(dst), uint64(seed), uint64(temporary)}, false
}

func TestIntegerHashStorePublicExecution(t *testing.T) {
	baselineTraps := make(map[string][]TrapFrame)
	for _, bound := range []uint32{2, 4, 8, 10, 18, 32, 0xfffffff0, 0xffffffff} {
		for _, transform := range []struct {
			x, m, mask uint32
			shift      int32
		}{{40503, 2246822507, 255, 13}, {0xffffffff, 0x80000000, 0xffffffff, -1}, {0, 0, 0, 32}, {17, 31, 0x12345678, 77}} {
			for _, offset := range []uint32{0, 7} {
				for _, nested := range []bool{false, true} {
					for _, on := range []bool{false, true} {
						for _, regABI := range []bool{false, true} {
							t.Run(fmt.Sprintf("bound=%x/hash=%v/off=%d/nested=%t/on=%t/abi=%t", bound, transform, offset, nested, on, regABI), func(t *testing.T) {
								cfg := NewRuntimeConfig().WithOptimization("reg-abi", regABI)
								if runtime.GOARCH == "arm64" {
									cfg = cfg.WithOptimization("bulk-fill-vector", on)
								}
								c, err := cfg.Compile(integerHashStoreModule(transform.x, transform.m, transform.mask, bound, offset, transform.shift, nested))
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
									mem := in.Memory().UnsafeBytes()
									initial := make([]byte, len(mem))
									for i := range initial {
										initial[i] = byte(i*37 ^ (i >> 4))
									}
									edge := uint32(len(mem)) - 8 - offset
									partial := uint32(len(mem)) - 4 - offset
									for _, args := range [][2]uint32{{0, 0}, {3, 0}, {0, 2}, {0, bound - 2}, {edge, bound - 2}, {partial, bound - 2}, {uint32(len(mem)), 0}, {0xfffffffc, 0}, {0, bound}, {0, 1}, {0, 0xfffffffe}} {
										copy(mem, initial)
										wantMem := bytes.Clone(initial)
										want, traps := integerHashStoreOracle(wantMem, args[0], args[1], transform.x, transform.m, transform.mask, bound, offset, transform.shift, nested)
										// Noncanonical host slot padding must not change guest i32 semantics.
										got, err := in.Invoke("run", uint64(args[0])|0xabcdef0000000000, uint64(args[1])|0xfedcba0000000000)
										if traps {
											var trap *TrapError
											if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
												t.Fatalf("args=%v want memory trap got=%v err=%v", args, got, err)
											}
											key := fmt.Sprintf("%x/%v/%d/%t/%t/%v", bound, transform, offset, nested, regABI, args)
											if !on {
												baselineTraps[key] = slices.Clone(trap.Frames)
											} else if !slices.Equal(trap.Frames, baselineTraps[key]) {
												t.Fatalf("fallback trap frames got=%v want=%v", trap.Frames, baselineTraps[key])
											}
										} else if err != nil || !slices.Equal(got, want) {
											t.Fatalf("args=%v got=%x want=%x err=%v", args, got, want, err)
										}
										if !bytes.Equal(mem, wantMem) {
											t.Fatalf("args=%v memory or partial writes differ", args)
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
