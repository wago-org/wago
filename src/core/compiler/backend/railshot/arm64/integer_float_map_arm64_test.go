//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func integerFloatMapFixture(t testing.TB, conversion byte, unroll int, reverse bool, count, dest, bias, offset, seed uint32) *wasm.Module {
	width, store, typ := uint32(4), byte(0x38), byte(0x7d)
	if conversion == 0xb7 || conversion == 0xb8 {
		width, store, typ = 8, 0x39, 0x7c
	}
	body := []byte{2, 4, 0x7f, 1, typ}
	constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	for i, x := range []uint32{0, dest, count} {
		constant(x)
		body = append(body, 0x21, byte(i))
	}
	body = append(body, 0x03, 0x40)
	for j := 0; j < unroll; j++ {
		lane := j
		if reverse {
			lane = unroll - 1 - j
		}
		body = append(body, 0x20, 1)
		constant(bias + width*uint32(lane))
		body = append(body, 0x6a)
		body = append(body, 0x20, 0)
		constant(seed + uint32(lane))
		body = append(body, 0x6a)
		constant(0)
		body = append(body, 0x73, 0x22, 3, conversion, 0x22, 4, store, 0)
		body = append(body, wasmtest.ULEB(offset)...)
	}
	body = append(body, 0x20, 1)
	constant(width * uint32(unroll))
	body = append(body, 0x6a, 0x21, 1)
	body = append(body, 0x20, 0)
	constant(uint32(unroll))
	body = append(body, 0x6a, 0x22, 0, 0x20, 2, 0x47, 0x0d, 0, 0x0b)
	// Keep the final floating temporary observable outside the candidate.
	constant(0)
	body = append(body, 0x20, 4, store, 0, 0, 0x20, 0, 0x20, 1, 0x73, 0x20, 3, 0x73, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func integerFloatBits(conversion byte, x uint32) uint64 {
	switch conversion {
	case 0xb2:
		return uint64(math.Float32bits(float32(int32(x))))
	case 0xb3:
		return uint64(math.Float32bits(float32(x)))
	case 0xb7:
		return math.Float64bits(float64(int32(x)))
	case 0xb8:
		return math.Float64bits(float64(x))
	}
	panic("invalid conversion")
}

func TestIntegerFloatMapExactConversionAndExitState(t *testing.T) {
	old, oldWord := integerFloatMapEnabled, integerWordMapEnabled
	defer func() { integerFloatMapEnabled, integerWordMapEnabled = old, oldWord }()
	integerWordMapEnabled = true
	for _, conversion := range []byte{0xb2, 0xb3, 0xb7, 0xb8} {
		width := uint32(4)
		if conversion >= 0xb7 {
			width = 8
		}
		for _, unroll := range []int{1, 2, 4} {
			for _, reverse := range []bool{false, true} {
				for _, count := range []uint32{uint32(unroll), uint32(5 * unroll), uint32(9 * unroll)} {
					for _, seed := range []uint32{0, 0xfffffffc, 0x7ffffffc, 0x80000000, 0x01000001} {
						for _, tc := range []struct{ dest, bias, offset uint32 }{{128, 0, 0}, {131, 4, 17}, {65520, 0, 0}, {0xfffffff0, 32, 0}, {0xfffffff0, 0, 32}} {
							m := integerFloatMapFixture(t, conversion, unroll, reverse, count, tc.dest, tc.bias, tc.offset, seed)
							initial := bytes.Repeat([]byte{0xa5}, 65536)
							var want uint32
							var wantMemory []byte
							var wantError string
							for _, enabled := range []bool{false, true} {
								integerFloatMapEnabled = enabled
								got, mem, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true}, initial, "integer-word-map-vector")
								if !enabled {
									want, wantMemory, wantError = got, mem, fmt.Sprint(err)
								} else if got != want || !bytes.Equal(mem, wantMemory) || fmt.Sprint(err) != wantError || diagnosticsEnabled && hits != 1 {
									t.Fatalf("conversion=%x unroll=%d reverse=%v count=%d seed=%x case=%+v got=%x want=%x err=%v expected=%s hits=%d", conversion, unroll, reverse, count, seed, tc, got, want, err, wantError, hits)
								}
							}
							oracle := append([]byte(nil), initial...)
							trapped := false
							var last uint32
							put := func(addr uint64, bits uint64) {
								if width == 4 {
									binary.LittleEndian.PutUint32(oracle[addr:], uint32(bits))
								} else {
									binary.LittleEndian.PutUint64(oracle[addr:], bits)
								}
							}
							for base := uint32(0); base < count && !trapped; base += uint32(unroll) {
								for j := 0; j < unroll; j++ {
									lane := j
									if reverse {
										lane = unroll - 1 - j
									}
									last = base + uint32(lane) + seed
									addr := uint64(tc.dest+width*base+tc.bias+width*uint32(lane)) + uint64(tc.offset)
									if addr+uint64(width) > uint64(len(oracle)) {
										trapped = true
										break
									}
									put(addr, integerFloatBits(conversion, last))
								}
							}
							if !trapped {
								put(0, integerFloatBits(conversion, last))
							}
							if !bytes.Equal(oracle, wantMemory) || (wantError != "<nil>") != trapped {
								t.Fatal("independent float oracle", conversion, unroll, reverse, count, seed, tc, wantError)
							}
						}
					}
				}
			}
		}
	}
}

func TestIntegerFloatMapCanonicalIntervalBoundary(t *testing.T) {
	oldFloat, oldWord, oldIntervals := integerFloatMapEnabled, integerWordMapEnabled, integerMapIntervalsEnabled
	defer func() {
		integerFloatMapEnabled, integerWordMapEnabled, integerMapIntervalsEnabled = oldFloat, oldWord, oldIntervals
	}()
	integerFloatMapEnabled, integerWordMapEnabled = true, true
	for _, conversion := range []byte{0xb2, 0xb3, 0xb7, 0xb8} {
		for _, unroll := range []int{1, 2, 4} {
			for _, reverse := range []bool{false, true} {
				for _, dest := range []uint32{128, 131, 65520} {
					m := integerFloatMapFixture(t, conversion, unroll, reverse, uint32(9*unroll), dest, 0, 0, 0xfffffffc)
					// Exercise dense candidate cells with sparse source-local IDs.
					// The large table and body admit source interval residency.
					body := m.Code[0].BodyBytes
					r := wasm.ReaderFrom(body)
					classifier := wasm.NewModuleInstructionClassifier(m, true)
					var imm wasm.InstructionImmediate
					var relocated []byte
					for r.Offset() < len(body) {
						at := r.Offset()
						op, err := r.Byte()
						if err != nil {
							t.Fatal(err)
						}
						if op == 0x20 || op == 0x21 || op == 0x22 {
							x, err := r.U32()
							if err != nil {
								t.Fatal(err)
							}
							relocated = append(relocated, op)
							relocated = append(relocated, wasmtest.ULEB(x+64)...)
						} else {
							if err = classifier.ClassifyInto(&r, op, &imm); err != nil {
								t.Fatal(err)
							}
							relocated = append(relocated, body[at:r.Offset()]...)
						}
					}
					relocated = append(relocated[:len(relocated)-1], bytes.Repeat([]byte{1}, 128)...)
					m.Code[0].BodyBytes = append(relocated, 0x0b)
					m.Code[0].Locals.Runs = append([]wasm.LocalRun{{Count: 64, Type: wasm.I32}}, m.Code[0].Locals.Runs...)
					m.Code[0].Locals.Runs = append(m.Code[0].Locals.Runs, wasm.LocalRun{Count: 32, Type: wasm.I32})
					var want uint32
					var wantMemory []byte
					var wantError string
					for _, enabled := range []bool{false, true} {
						integerMapIntervalsEnabled = enabled
						got, mem, err, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true, "interval-region-pins": true}, nil, "integer-word-map-vector")
						if !enabled {
							want, wantMemory, wantError = got, mem, fmt.Sprint(err)
							if diagnosticsEnabled && hits != 0 {
								t.Fatal("fixture lacks source interval residency", hits)
							}
						} else if got != want || !bytes.Equal(mem, wantMemory) || fmt.Sprint(err) != wantError || diagnosticsEnabled && hits != 1 {
							t.Fatal("interval-boundary state", conversion, unroll, reverse, dest, got, want, err, wantError, hits)
						}
					}
				}
			}
		}
	}
}
