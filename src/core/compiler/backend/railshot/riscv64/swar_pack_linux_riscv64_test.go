//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func swarNarrowOracle(input []byte, width int, signed bool) [16]byte {
	var out [16]byte
	low, high := int64(0), int64(255)
	if width == 2 {
		high = 65535
	}
	if signed {
		if width == 1 {
			low, high = -128, 127
		} else {
			low, high = -32768, 32767
		}
	}
	for lane := 0; lane < 16/width; lane++ {
		value := swarWidenLane(input[lane*2*width:], width*2, true)
		if value < low {
			value = low
		} else if value > high {
			value = high
		}
		if width == 1 {
			out[lane] = byte(value)
		} else {
			binary.LittleEndian.PutUint16(out[lane*2:], uint16(value))
		}
	}
	return out
}

func swarExtmultiplyOracle(input []byte, width int, signed, high bool) [16]byte {
	// Independently extend source bytes, then apply schoolbook byte products at
	// the doubled width. This also avoids overflow in host signed64 arithmetic.
	a := swarWidenOracle(input[:16], width, signed, high, false)
	b := swarWidenOracle(input[16:], width, signed, high, false)
	return swarProductOracle(a[:], b[:], 2*width, swarMul)
}

func TestSWARRV64NarrowAndExtmultiplyAllLanes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	rng := rand.New(rand.NewSource(640101223))
	tail := bytes.Repeat([]byte{0xa5}, 16)
	for _, kind := range swarPackKinds {
		width, signed, narrow := swarNarrowOpcode(kind)
		high := false
		if !narrow {
			width, signed, high, _ = swarExtmultiplyOpcode(kind)
		}
		laneBytes := int(width) / 8
		sourceBytes := laneBytes
		if narrow {
			sourceBytes *= 2
		}
		fixture := swarArithmeticFixture(kind)
		fixture.locals = []byte{1, 126, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large narrowing/extmultiply frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		for sample := 0; sample < 64; sample++ {
			var input [32]byte
			_, _ = rng.Read(input[:])
			if narrow && sample < 16 {
				limits := []int64{-2147483648, -65536, -32769, -32768, -129, -128, -1, 0, 1, 127, 128, 255, 256, 32766, 32767, 32768, 65534, 65535, 65536, 2147483647}
				for lane := 0; lane < 32/sourceBytes; lane++ {
					value := limits[(sample+lane)%len(limits)]
					if sourceBytes == 2 {
						binary.LittleEndian.PutUint16(input[lane*2:], uint16(value))
					} else {
						binary.LittleEndian.PutUint32(input[lane*4:], uint32(value))
					}
				}
			} else if !narrow && sample < 8 {
				clear(input[:])
				for start := 0; start < 16; start += sourceBytes {
					switch sample {
					case 1:
						for i := 0; i < sourceBytes; i++ {
							input[start+i], input[16+start+i] = 255, 255
						}
					case 2:
						input[start+sourceBytes-1], input[16+start] = 0x80, 1
					case 3:
						input[start], input[16+start+sourceBytes-1] = 1, 0x80
					case 4:
						for i := 0; i < sourceBytes; i++ {
							input[start+i], input[16+start+i] = byte(start+i+1), byte(32-start-i)
						}
					case 5:
						if start < 8 {
							input[start+sourceBytes-1], input[16+start] = 0x80, 1
						} else {
							input[start], input[16+start+sourceBytes-1] = 1, 0x80
						}
					case 6:
						if start < 8 {
							input[start], input[16+start+sourceBytes-1] = 1, 0x80
						} else {
							input[start+sourceBytes-1], input[16+start] = 0x80, 1
						}
					case 7:
						input[start+sourceBytes-1], input[16+start+sourceBytes-1] = 0x80, 0x80
					}
				}
			} else if sample == 16 {
				binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(input[8:16], 0x8000000000000000)
				binary.LittleEndian.PutUint64(input[16:24], 0xffffffffffffffff)
				binary.LittleEndian.PutUint64(input[24:], 1)
			}
			copy(args, input[:])
			copy(results[16:32], tail)
			var want [16]byte
			if narrow {
				want = swarNarrowOracle(input[:], laneBytes, signed)
			} else {
				want = swarExtmultiplyOracle(input[:], laneBytes, signed, high)
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) || !bytes.Equal(results[16:32], tail) {
				t.Fatal("narrow saturation/extmultiply sign-half", kind, sample, input, results[:32], want, err)
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("pack allocations", kind, allocs)
		}
	}
}

func TestSWARRV64NarrowEverySigned16Input(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, kind := range []uint32{101, 102} {
		_, signed, _ := swarNarrowOpcode(kind)
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		for base := 0; base < 65536; base += 16 {
			for lane := 0; lane < 16; lane++ {
				binary.LittleEndian.PutUint16(args[lane*2:], uint16(base+lane))
			}
			want := swarNarrowOracle(args[:32], 1, signed)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) {
				t.Fatal("every signed source narrow", kind, base, results[:16], want, err)
			}
		}
	}
}

func TestSWARRV64ByteExtmultiplyExhaustive(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, kind := range []uint32{156, 157, 158, 159} {
		_, signed, high, _ := swarExtmultiplyOpcode(kind)
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		start := 0
		if high {
			start = 8
		}
		for a := 0; a < 256; a++ {
			for b := 0; b < 256; b += 8 {
				for i := 0; i < 32; i++ {
					args[i] = byte(31 + i)
				}
				for lane := 0; lane < 8; lane++ {
					args[start+lane], args[16+start+lane] = byte(a), byte(b+lane)
				}
				want := swarExtmultiplyOracle(args[:32], 1, signed, high)
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) {
					t.Fatal("exhaustive byte extmultiply", kind, a, b, results[:16], want, err)
				}
			}
		}
	}
}
