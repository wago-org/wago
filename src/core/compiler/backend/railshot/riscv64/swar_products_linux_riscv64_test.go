//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

// Multiplication uses bytewise schoolbook products with carry reset at each
// lane. Min/max chooses the original lane bytes through direct comparisons.
func swarProductOracle(a, b []byte, width int, operation swarProduct) [16]byte {
	var out [16]byte
	for start := 0; start < 16; start += width {
		if operation == swarMul {
			for i := 0; i < width; i++ {
				carry := uint32(0)
				for j := 0; i+j < width; j++ {
					value := uint32(out[start+i+j]) + uint32(a[start+i])*uint32(b[start+j]) + carry
					out[start+i+j] = byte(value)
					carry = value >> 8
				}
			}
			continue
		}
		var av, bv uint64
		for i := 0; i < width; i++ {
			av |= uint64(a[start+i]) << (8 * i)
			bv |= uint64(b[start+i]) << (8 * i)
		}
		chooseA := false
		switch operation {
		case swarMinS:
			chooseA = swarSignedLane(av, width*8) < swarSignedLane(bv, width*8)
		case swarMinU:
			chooseA = av < bv
		case swarMaxS:
			chooseA = swarSignedLane(av, width*8) > swarSignedLane(bv, width*8)
		case swarMaxU:
			chooseA = av > bv
		}
		source := b
		if chooseA {
			source = a
		}
		copy(out[start:start+width], source[start:start+width])
	}
	return out
}

func TestSWARRV64MultiplyAndMinMaxAllLanes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	rng := rand.New(rand.NewSource(640149213))
	for _, kind := range swarProductKinds {
		width, operation, _ := swarProductOpcode(kind)
		laneBytes := int(width) / 8
		fixture := swarArithmeticFixture(kind)
		fixture.locals = []byte{1, 126, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large product frame", kind, info, err)
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
			} else if sample == 9 {
				copy(input[16:], input[:16])
			} else if sample == 10 {
				copy(input[16:], input[:16])
				for start := laneBytes; start < 16; start += 2 * laneBytes {
					input[16+start] ^= 1
				}
			}
			copy(args, input[:])
			want := swarProductOracle(input[:16], input[16:], laneBytes, operation)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
				t.Fatal("lane product/minmax", kind, sample, input, results[:16], want, err)
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("product native allocations", kind, allocs)
		}
	}
}

func TestSWARRV64ByteMinMaxExhaustive(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, kind := range []uint32{118, 119, 120, 121} {
		_, operation, _ := swarProductOpcode(kind)
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
				want := swarProductOracle(args[:16], args[16:32], 1, operation)
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) {
					t.Fatal("exhaustive byte minmax", kind, a, b, results[:16], want, err)
				}
			}
		}
	}
}
