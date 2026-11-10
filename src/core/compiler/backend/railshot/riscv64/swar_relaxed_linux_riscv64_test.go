//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64RelaxedCoreDeterministicOutcomes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 6)
	rng := rand.New(rand.NewSource(64256273))
	for _, test := range swarRelaxedCoreCases {
		fixture := swarRelaxedFixture(test.relaxed)
		fixture.locals = []byte{1, byte(128 - len(fixture.params)), 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large relaxed frame", test, info, err)
		}
		entry := thunkImageRV64(t, code)
		wide := test.relaxed == 259 || test.relaxed == 260 || test.relaxed == 271 || test.relaxed == 272
		values := floatInputs(wide)
		samples := 256
		if test.relaxed >= 269 && test.relaxed <= 272 {
			samples = len(values) * len(values)
		}
		for sample := 0; sample < samples; sample++ {
			var input [48]byte
			var want [16]byte
			_, _ = rng.Read(input[:])
			switch {
			case test.relaxed == 256:
				for lane := 0; lane < 16; lane++ {
					input[16+lane] = byte(sample + lane)
				}
				for lane := 0; lane < 16; lane++ {
					if index := input[16+lane]; index < 16 {
						want[lane] = input[index]
					}
				}
			case test.relaxed >= 257 && test.relaxed <= 260:
				laneBytes := 4
				if wide {
					laneBytes = 8
				}
				for lane := 0; lane < 16/laneBytes; lane++ {
					value := values[(sample+lane)%len(values)]
					if wide {
						binary.LittleEndian.PutUint64(input[lane*8:], value)
					} else {
						binary.LittleEndian.PutUint32(input[lane*4:], uint32(value))
					}
					binary.LittleEndian.PutUint32(want[lane*4:], uint32(swarVectorConversionExpected(test.core, value)))
				}
			case test.relaxed >= 265 && test.relaxed <= 268:
				for lane := 0; lane < 16; lane++ {
					input[32+lane] = byte(sample + lane)
					want[lane] = input[lane]&input[32+lane] | input[16+lane]&^input[32+lane]
				}
			case test.relaxed >= 269 && test.relaxed <= 272:
				laneBytes, width := 4, uint8(32)
				if wide {
					laneBytes, width = 8, 64
				}
				for lane := 0; lane < 16/laneBytes; lane++ {
					a, b := values[(sample%len(values)+lane)%len(values)], values[(sample/len(values)+2*lane)%len(values)]
					value := swarFloatMinMaxOracle(a, b, width, test.relaxed == 270 || test.relaxed == 272, false)
					if wide {
						binary.LittleEndian.PutUint64(input[lane*8:], a)
						binary.LittleEndian.PutUint64(input[16+lane*8:], b)
						binary.LittleEndian.PutUint64(want[lane*8:], value)
					} else {
						binary.LittleEndian.PutUint32(input[lane*4:], uint32(a))
						binary.LittleEndian.PutUint32(input[16+lane*4:], uint32(b))
						binary.LittleEndian.PutUint32(want[lane*4:], uint32(value))
					}
				}
			case test.relaxed == 273:
				if sample < 8 {
					for lane := 0; lane < 8; lane++ {
						binary.LittleEndian.PutUint16(input[lane*2:], []uint16{0x8000, 0x7fff, 1, 0xffff}[sample%4])
						binary.LittleEndian.PutUint16(input[16+lane*2:], []uint16{0x8000, 0x7fff, 1, 0xffff}[(sample/4+lane)%4])
					}
				}
				want = swarSpecialMultiplyOracle(input[:16], input[16:32], 130)
			}
			copy(args, input[:])
			for i := 16; i < 32; i++ {
				results[i] = 0xa5
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:48], input[:]) {
				t.Fatal("relaxed deterministic outcome", test, sample, input, results[:16], want, err)
			}
			for i := 16; i < 32; i++ {
				if results[i] != 0xa5 {
					t.Fatal("relaxed result tail", test, i)
				}
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("relaxed allocations", test, allocs)
		}
	}
}
