//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

// The oracle computes carry/borrow one byte at a time and resets at each lane.
// It does not repeat the guard-mask algorithm used by the emitter.
func swarArithmeticOracle(a, b []byte, width int, subtract bool) [16]byte {
	var out [16]byte
	for lane := 0; lane < 16; lane += width {
		carry := 0
		for i := lane; i < lane+width; i++ {
			value := int(a[i]) + int(b[i]) + carry
			if subtract {
				value = int(a[i]) - int(b[i]) + carry
				carry = 0
				if value < 0 {
					carry = -1
				}
			} else {
				carry = value >> 8
			}
			out[i] = byte(value)
		}
	}
	return out
}

func TestSWARRV64WrappingArithmeticAllWidths(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	rng := rand.New(rand.NewSource(6408163264))
	for _, kind := range []uint32{110, 113, 142, 145, 174, 177, 206, 209} {
		width, subtract, _ := swarAddSubOpcode(kind)
		fixture := swarArithmeticFixture(kind)
		fixture.locals = []byte{1, 126, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large arithmetic frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		for sample := 0; sample < 64; sample++ {
			var input [32]byte
			_, _ = rng.Read(input[:])
			if sample < 8 {
				for i := 0; i < 16; i++ {
					switch sample {
					case 0:
						input[i], input[16+i] = 0, 0
					case 1:
						input[i], input[16+i] = 255, 255
					case 2:
						input[i], input[16+i] = 255, 0
					case 3:
						input[i], input[16+i] = 0, 255
					case 4:
						input[i], input[16+i] = 0x7f, 1
					case 5:
						input[i], input[16+i] = 0x80, 1
					case 6:
						input[i], input[16+i] = byte(i*17), byte(255-i*17)
					case 7:
						input[i], input[16+i] = byte(i*17), byte(i*17)
					}
				}
			} else if sample == 8 {
				binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(input[8:16], 0x8000000000000000)
				binary.LittleEndian.PutUint64(input[16:24], 0xffffffffffffffff)
				binary.LittleEndian.PutUint64(input[24:], 1)
			}
			copy(args, input[:])
			want := swarArithmeticOracle(input[:16], input[16:], int(width)/8, subtract)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
				t.Fatal("lane wrap/carry/borrow", kind, sample, input, results[:16], want, err)
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("arithmetic native allocations", kind, allocs)
		}
	}
}

func TestSWARRV64ByteArithmeticExhaustive(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, kind := range []uint32{110, 113} {
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
				want := swarArithmeticOracle(args[:16], args[16:32], 1, kind == 113)
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) {
					t.Fatal("exhaustive byte pair", kind, a, b, results[:16], want, err)
				}
			}
		}
	}
}
