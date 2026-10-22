//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func swarWidenLane(input []byte, width int, signed bool) int64 {
	switch width {
	case 1:
		if signed {
			return int64(int8(input[0]))
		}
		return int64(input[0])
	case 2:
		value := binary.LittleEndian.Uint16(input)
		if signed {
			return int64(int16(value))
		}
		return int64(value)
	case 4:
		value := binary.LittleEndian.Uint32(input)
		if signed {
			return int64(int32(value))
		}
		return int64(value)
	default:
		panic("invalid oracle width")
	}
}

func swarWidenOracle(input []byte, width int, signed, high, pairwise bool) [16]byte {
	var out [16]byte
	for lane := 0; lane < 16/(2*width); lane++ {
		position := lane * width
		if high {
			position += 8
		}
		if pairwise {
			position = lane * 2 * width
		}
		value := swarWidenLane(input[position:], width, signed)
		if pairwise {
			value += swarWidenLane(input[position+width:], width, signed)
		}
		switch width {
		case 1:
			binary.LittleEndian.PutUint16(out[lane*2:], uint16(value))
		case 2:
			binary.LittleEndian.PutUint32(out[lane*4:], uint32(value))
		case 4:
			binary.LittleEndian.PutUint64(out[lane*8:], uint64(value))
		}
	}
	return out
}

func TestSWARRV64WideningAndPairwiseAllLanes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 2)
	rng := rand.New(rand.NewSource(640124202))
	for _, kind := range swarWidenKinds {
		width, signed, high, pairwise, _ := swarWidenOpcode(kind)
		laneBytes := int(width) / 8
		fixture := swarUnaryFixture(kind, false)
		fixture.locals = []byte{1, 127, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large widening frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		for sample := 0; sample < 64; sample++ {
			var input [16]byte
			_, _ = rng.Read(input[:])
			if sample < 8 {
				clear(input[:])
				for start := 0; start < 16; start += laneBytes {
					switch sample {
					case 1:
						for i := 0; i < laneBytes; i++ {
							input[start+i] = 255
						}
					case 2:
						input[start+laneBytes-1] = 0x80
					case 3:
						for i := 0; i < laneBytes; i++ {
							input[start+i] = 255
						}
						input[start+laneBytes-1] = 0x7f
					case 4:
						for i := 0; i < laneBytes; i++ {
							input[start+i] = byte(start + i + 1)
						}
					case 5:
						if start < 8 {
							input[start+laneBytes-1] = 0x80
						} else {
							input[start] = 1
						}
					case 6:
						if start < 8 {
							input[start] = 1
						} else {
							input[start+laneBytes-1] = 0x80
						}
					case 7:
						if (start/laneBytes)%2 == 0 {
							input[start+laneBytes-1] = 0x80
						} else {
							for i := 0; i < laneBytes; i++ {
								input[start+i] = 255
							}
							input[start+laneBytes-1] = 0x7f
						}
					}
				}
			} else if sample == 8 {
				binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(input[8:], 0x8000000000000000)
			}
			copy(args, input[:])
			want := swarWidenOracle(input[:], laneBytes, signed, high, pairwise)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:16], input[:]) {
				t.Fatal("widen signedness/half/pair", kind, sample, input, results[:16], want, err)
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("widen allocations", kind, allocs)
		}
	}
}

func TestSWARRV64ByteWidenEveryValueAndPair(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 2)
	for _, kind := range []uint32{124, 125, 135, 136, 137, 138} {
		_, signed, high, pairwise, _ := swarWidenOpcode(kind)
		fn, ft := swarValidatedFunction(t, swarUnaryFixture(kind, false))
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		if pairwise {
			for a := 0; a < 256; a++ {
				for b := 0; b < 256; b += 8 {
					for lane := 0; lane < 8; lane++ {
						args[lane*2], args[lane*2+1] = byte(a), byte(b+lane)
					}
					want := swarWidenOracle(args[:16], 1, signed, false, true)
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) {
						t.Fatal("exhaustive byte pairwise", kind, a, b, results[:16], want, err)
					}
				}
			}
		} else {
			for base := 0; base < 256; base++ {
				for lane := 0; lane < 16; lane++ {
					args[lane] = byte(base + 17*lane)
				}
				want := swarWidenOracle(args[:16], 1, signed, high, false)
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) {
					t.Fatal("every byte extension", kind, base, results[:16], want, err)
				}
			}
		}
	}
}
