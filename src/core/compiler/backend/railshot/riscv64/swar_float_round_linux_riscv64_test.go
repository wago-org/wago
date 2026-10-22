//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64FloatingVectorIntegralRoundEdges(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for i, kind := range swarFloatRoundKinds {
		wide := i >= 4
		laneBytes := 4
		if wide {
			laneBytes = 8
		}
		fixture := swarUnaryFixture(kind, false)
		fixture.locals = []byte{1, 127, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large FP round frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		values := floatInputs(wide)
		if wide {
			values = append(values, 0x3fdfffffffffffff, 0x3fe0000000000000, 0x3fe0000000000001, 0xbfdfffffffffffff, 0xbfe0000000000000, 0xbfe0000000000001,
				0x433fffffffffffff, 0x4340000000000000, 0x4340000000000001, 0x43dfffffffffffff, 0x43e0000000000000, 0x43e0000000000001,
				0xc3dfffffffffffff, 0xc3e0000000000000, 0xc3e0000000000001)
		} else {
			values = append(values, 0x3effffff, 0x3f000000, 0x3f000001, 0xbeffffff, 0xbf000000, 0xbf000001,
				0x4affffff, 0x4b000000, 0x4b000001, 0x5effffff, 0x5f000000, 0x5f000001, 0xdeffffff, 0xdf000000, 0xdf000001)
		}
		for vi := range values {
			var input, want [16]byte
			for lane := 0; lane < 16/laneBytes; lane++ {
				value := values[(vi+lane)%len(values)]
				result := floatMathExpected(byte(2+i%4), value, 0, wide)
				if wide {
					binary.LittleEndian.PutUint64(input[lane*8:], value)
					binary.LittleEndian.PutUint64(want[lane*8:], result)
				} else {
					binary.LittleEndian.PutUint32(input[lane*4:], uint32(value))
					binary.LittleEndian.PutUint32(want[lane*4:], uint32(result))
				}
			}
			copy(args, input[:])
			for n := 16; n < 32; n++ {
				results[n] = 0xa5
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:16], input[:]) {
				t.Fatal("FP vector integral round", kind, vi, input, results[:16], want, err)
			}
			for n := 16; n < 32; n++ {
				if results[n] != 0xa5 {
					t.Fatal("FP round tail", kind, n)
				}
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("FP round allocations", kind, allocs)
		}
	}
}
