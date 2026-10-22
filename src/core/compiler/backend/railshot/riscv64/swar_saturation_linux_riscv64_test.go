//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

// The oracle uses widened Go arithmetic and explicit scalar limits, rather
// than the emitter's branch-free Boolean-mask clamp.
func swarSaturationOracle(a, b []byte, width int, operation swarSaturation) [16]byte {
	var out [16]byte
	for start := 0; start < 16; start += width {
		av, bv := int64(a[start]), int64(b[start])
		if width == 2 {
			av = int64(binary.LittleEndian.Uint16(a[start:]))
			bv = int64(binary.LittleEndian.Uint16(b[start:]))
		}
		low, high := int64(0), int64(255)
		if width == 2 {
			high = 65535
		}
		if operation == swarAddSatS || operation == swarSubSatS {
			if width == 1 {
				av = int64(int8(av))
				bv = int64(int8(bv))
				low, high = -128, 127
			} else {
				av = int64(int16(av))
				bv = int64(int16(bv))
				low, high = -32768, 32767
			}
		}
		value := av + bv
		if operation == swarSubSatS || operation == swarSubSatU {
			value = av - bv
		}
		if operation == swarAvgrU {
			value = (value + 1) / 2
		} else if value < low {
			value = low
		} else if value > high {
			value = high
		}
		if width == 1 {
			out[start] = byte(value)
		} else {
			binary.LittleEndian.PutUint16(out[start:], uint16(value))
		}
	}
	return out
}

func TestSWARRV64SaturationAndAverageAllLanes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	rng := rand.New(rand.NewSource(640111155))
	for _, kind := range swarSaturationKinds {
		width, operation, _ := swarSaturationOpcode(kind)
		laneBytes := int(width) / 8
		fixture := swarArithmeticFixture(kind)
		fixture.locals = []byte{1, 126, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large saturation frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		for sample := 0; sample < 64; sample++ {
			var input [32]byte
			_, _ = rng.Read(input[:])
			if sample < 8 {
				clear(input[:])
				for start := 0; start < 16; start += laneBytes {
					switch sample {
					case 0:
					case 1:
						for i := 0; i < laneBytes; i++ {
							input[start+i], input[16+start+i] = 255, 255
						}
					case 2:
						input[start+laneBytes-1], input[16+start] = 0x80, 1
					case 3:
						input[start], input[16+start+laneBytes-1] = 1, 0x80
					case 4:
						for i := 0; i < laneBytes; i++ {
							input[start+i], input[16+start+i] = 255, 255
						}
						input[start+laneBytes-1] = 0x7f
					case 5:
						for i := 0; i < laneBytes; i++ {
							input[start+i], input[16+start+i] = byte(start+i), byte(15-start-i)
						}
					case 6:
						for i := 0; i < laneBytes; i++ {
							input[start+i], input[16+start+i] = 255, 1
						}
					case 7:
						input[start+laneBytes-1], input[16+start+laneBytes-1] = 0x80, 0x80
					}
				}
			} else if sample == 8 {
				binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(input[8:16], 0x8000000000000000)
				binary.LittleEndian.PutUint64(input[16:24], 0xffffffffffffffff)
				binary.LittleEndian.PutUint64(input[24:], 1)
			} else if sample == 12 || sample == 13 {
				for start := 0; start < 16; start += laneBytes {
					for i := 0; i < laneBytes; i++ {
						input[start+i], input[16+start+i] = 255, 255
					}
					input[start+laneBytes-1] = 0x7f
					if sample == 12 {
						input[16+start+laneBytes-1] = 0x7f
					} else {
						clear(input[start : start+laneBytes])
						input[start+laneBytes-1] = 0x80
					}
				}
			} else if sample == 9 {
				copy(input[16:], input[:16])
			} else if sample == 10 {
				copy(input[16:], input[:16])
				for start := laneBytes; start < 16; start += 2 * laneBytes {
					input[16+start] ^= 1
				}
			}
			copy(args, input[:])
			want := swarSaturationOracle(input[:16], input[16:], laneBytes, operation)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
				t.Fatal("lane saturation/average", kind, sample, input, results[:16], want, err)
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("saturation native allocations", kind, allocs)
		}
	}
}

func TestSWARRV64ByteSaturationAndAverageExhaustive(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, kind := range []uint32{111, 112, 114, 115, 123} {
		_, operation, _ := swarSaturationOpcode(kind)
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		for a := 0; a < 256; a++ {
			for b := 0; b < 256; b += 16 {
				for lane := 0; lane < 16; lane++ {
					args[lane], args[16+lane] = byte(a), byte(b+lane)
				}
				want := swarSaturationOracle(args[:16], args[16:32], 1, operation)
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) {
					t.Fatal("exhaustive byte saturation/average", kind, a, b, results[:16], want, err)
				}
			}
		}
	}
}
