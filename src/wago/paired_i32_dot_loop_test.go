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

func pairedI32DotModule(leftBias, rightBias int32, bound, offset uint32, nested bool) []byte {
	b := []byte{1, 3, 0x7f}
	if nested {
		b = append(b, 0x03, 0x40, 0x41, 0, 0x21, 2)
	}
	b = append(b, 0x03, 0x40, 0x20, 0, 0x20, 2, 0x6a, 0x22, 4, 0x41, 4, 0x6a, 0x28, 2)
	b = append(b, wasmtest.ULEB(offset)...)
	b = append(b, 0x41)
	b = append(b, wasmtest.SLEB64(int64(leftBias))...)
	b = append(b, 0x6a, 0x20, 1, 0x20, 2, 0x6a, 0x22, 5, 0x41, 4, 0x6a, 0x28, 2)
	b = append(b, wasmtest.ULEB(offset)...)
	b = append(b, 0x41)
	b = append(b, wasmtest.SLEB64(int64(rightBias))...)
	b = append(b, 0x6a, 0x6c, 0x20, 4, 0x28, 2)
	b = append(b, wasmtest.ULEB(offset)...)
	b = append(b, 0x41)
	b = append(b, wasmtest.SLEB64(int64(leftBias))...)
	b = append(b, 0x6a, 0x20, 5, 0x28, 2)
	b = append(b, wasmtest.ULEB(offset)...)
	b = append(b, 0x41)
	b = append(b, wasmtest.SLEB64(int64(rightBias))...)
	b = append(b, 0x6a, 0x6c, 0x20, 3, 0x6a, 0x6a, 0x21, 3, 0x20, 2, 0x41, 8, 0x6a, 0x22, 2, 0x41)
	b = append(b, wasmtest.SLEB64(int64(bound))...)
	b = append(b, 0x47, 0x0d, 0, 0x0b)
	if nested {
		b = append(b, 0x20, 6, 0x41, 1, 0x6a, 0x22, 6, 0x41, 3, 0x49, 0x0d, 0, 0x0b)
	}
	b = append(b, 0x20, 3, 0x20, 2, 0x20, 4, 0x20, 5, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...))))
}

func pairedI32DotOracle(mem []byte, left, right, counter, acc, bound, offset uint32, lb, rb int32, nested bool) ([]uint64, bool) {
	reps := 1
	if nested {
		reps = 3
	}
	var la, ra uint32
	load := func(address uint32) (uint32, bool) {
		start := uint64(address) + uint64(offset)
		if start+4 > uint64(len(mem)) {
			return 0, false
		}
		return binary.LittleEndian.Uint32(mem[int(start):]), true
	}
	for rep := 0; rep < reps; rep++ {
		if nested {
			counter = 0
		}
		for iterations := 0; iterations < 10000; iterations++ {
			la, ra = left+counter, right+counter
			lh, ok := load(la + 4)
			if !ok {
				return nil, true
			}
			rh, ok := load(ra + 4)
			if !ok {
				return nil, true
			}
			ll, ok := load(la)
			if !ok {
				return nil, true
			}
			rl, ok := load(ra)
			if !ok {
				return nil, true
			}
			acc += (lh+uint32(lb))*(rh+uint32(rb)) + (ll+uint32(lb))*(rl+uint32(rb))
			counter += 8
			if counter == bound {
				break
			}
			if iterations == 9999 {
				panic("test oracle did not terminate or trap")
			}
		}
	}
	return []uint64{uint64(acc), uint64(counter), uint64(la), uint64(ra)}, false
}

func TestPairedI32DotPublicExecution(t *testing.T) {
	baselineTraps := make(map[string][]TrapFrame)
	for _, bound := range []uint32{8, 16, 24, 32, 40, 48, 56, 64, 72, 80, 88, 96, 104, 112, 120, 128, 136, 144, 152, 160, 168, 176, 184, 192, 200, 208, 216, 224, 232, 240, 248, 256, 264, 272, 280, 288, 296, 304, 312, 320, 328, 336, 344, 352, 360, 368, 376, 384, 392, 400, 408, 416, 424, 432, 440, 448, 456, 464, 472, 480, 488, 496, 504, 512} {
		for _, bias := range [][2]int32{{-128, -128}, {2147483647, -2147483648}, {0, 1}} {
			for _, offset := range []uint32{0, 7, ^uint32(0)} {
				for _, nested := range []bool{false, true} {
					for _, on := range []bool{false, true} {
						for _, regABI := range []bool{false, true} {
							t.Run(fmt.Sprintf("bound=%d/bias=%v/off=%d/nested=%t/on=%t/abi=%t", bound, bias, offset, nested, on, regABI), func(t *testing.T) {
								cfg := NewRuntimeConfig().WithOptimization("reg-abi", regABI)
								if runtime.GOARCH == "arm64" {
									cfg = cfg.WithOptimization("integer-sum-loop", on)
								}
								c, err := cfg.Compile(pairedI32DotModule(bias[0], bias[1], bound, offset, nested))
								if err != nil {
									t.Fatal(err)
								}
								defer c.Close()
								compiledCases := []*Compiled{c}
								// Guard-page images cannot be serialized; still execute the original.
								if !guardPageBuilt {
									loaded := publicArtifactRoundTrip(t, c)
									defer loaded.Close()
									compiledCases = append(compiledCases, loaded)
								}
								for _, compiled := range compiledCases {
									in, err := Instantiate(compiled)
									if err != nil {
										t.Fatal(err)
									}
									defer in.Close()
									mem := in.Memory().UnsafeBytes()
									for i := range mem {
										mem[i] = byte((i * 37) ^ (i >> 3) ^ (i >> 9))
									}
									before := bytes.Clone(mem)
									edge := uint32(len(mem)) - bound - offset
									wrapEdge := uint32((uint64(1) << 32) - uint64(bound))
									for _, args := range [][3]uint32{{wrapEdge - 1, 64, 0}, {wrapEdge, 64, 0}, {wrapEdge + 1, 64, 0}, {wrapEdge, 64, bound - 8}, {wrapEdge + 8, 64, bound - 8}, {0, 64, 0}, {3, 129, 0}, {64, 64, 0}, {edge, edge, 0}, {0, 64, 8}, {0, 64, 16}, {0, 64, bound}, {0, 64, 1}, {^uint32(15), 32, 0}, {^uint32(15), 32, 16}, {uint32(len(mem)) - 4, 64, 0}, {0, 64, ^uint32(3)}} {
										seed := uint32(0xfedcba98)
										want, traps := pairedI32DotOracle(before, args[0], args[1], args[2], seed, bound, offset, bias[0], bias[1], nested)
										// Exercise canonical and padded i32 carriers under both ABIs.
										padding := uint64(0)
										if args[2]&8 == 0 {
											padding = 0xfedcba9800000000
										}
										got, err := in.Invoke("run", uint64(args[0])|padding, uint64(args[1])|padding, uint64(args[2])|padding, uint64(seed)|padding)
										if traps {
											var trap *TrapError
											if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
												t.Fatalf("args=%v expected trap got=%v err=%v", args, got, err)
											}
											key := fmt.Sprintf("%d/%v/%d/%t/%t/%v", bound, bias, offset, nested, regABI, args)
											if !on {
												baselineTraps[key] = slices.Clone(trap.Frames)
											} else if !slices.Equal(trap.Frames, baselineTraps[key]) {
												t.Fatalf("fallback changed trap frames: got=%v want=%v", trap.Frames, baselineTraps[key])
											}
										} else if err != nil || !slices.Equal(got, want) {
											t.Fatalf("args=%v got=%x want=%x err=%v", args, got, want, err)
										}
									}
									if !bytes.Equal(mem, before) {
										t.Fatal("dot loop changed memory")
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
