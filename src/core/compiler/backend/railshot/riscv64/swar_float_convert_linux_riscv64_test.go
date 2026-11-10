//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math"
	goruntime "runtime"
	"testing"
)

func swarVectorConversionExpected(kind uint32, bits uint64) uint64 {
	switch kind {
	case 94, 95:
		inputWide, outputWide := kind == 94, kind == 95
		value := floatValue(bits, inputWide)
		if math.IsNaN(value) {
			if outputWide {
				return 0x7ff8000000000000
			}
			return 0x7fc00000
		}
		return floatBits(value, outputWide)
	case 248, 249, 252, 253:
		result, traps := truncExpected(floatValue(bits, kind >= 252), false, kind&1 != 0, true)
		if traps {
			panic("saturating conversion oracle trapped")
		}
		return result
	case 250:
		return uint64(math.Float32bits(float32(int32(bits))))
	case 251:
		return uint64(math.Float32bits(float32(uint32(bits))))
	case 254:
		return math.Float64bits(float64(int32(bits)))
	case 255:
		return math.Float64bits(float64(uint32(bits)))
	default:
		panic("conversion oracle opcode")
	}
}

func TestSWARRV64FloatingVectorConversionsSaturationAndZeroLanes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, kind := range swarFloatConvertKinds {
		inputBytes, outputBytes, lanes := 4, 4, 4
		if kind == 94 || kind == 252 || kind == 253 {
			inputBytes, lanes = 8, 2
		}
		if kind == 95 || kind == 254 || kind == 255 {
			outputBytes, lanes = 8, 2
		}
		fixture := swarUnaryFixture(kind, false)
		fixture.locals = []byte{1, 127, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large vector conversion frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		values := []uint64{0, 1, 0xffffffff, 0x80000000, 0x7fffffff, 0x1000000, 0x1000001, 0x1000003, 0x7fffff80, 0x7fffffc0, 0xffffff00, 0xffffff80, 0x80000001}
		if kind == 94 || kind == 95 || kind == 248 || kind == 249 || kind == 252 || kind == 253 {
			values = floatInputs(inputBytes == 8)
			for _, value := range []float64{-0.999, -1.001, -2147483648.75, 2147483647.75, 4294967295.75, -2147483649, 4294967296} {
				values = append(values, floatBits(value, inputBytes == 8))
			}
			for _, edge := range []float64{-2147483648, 2147483648, 4294967296} {
				if inputBytes == 8 {
					values = append(values, floatBits(math.Nextafter(edge, math.Inf(-1)), true), floatBits(edge, true), floatBits(math.Nextafter(edge, math.Inf(1)), true))
				} else {
					values = append(values, uint64(math.Float32bits(math.Nextafter32(float32(edge), float32(math.Inf(-1))))), floatBits(edge, false), uint64(math.Float32bits(math.Nextafter32(float32(edge), float32(math.Inf(1))))))
				}
			}
			if kind == 94 {
				values = append(values, 0x3ff000000fffffff, 0x3ff0000010000000, 0x3ff0000010000001, 0x368fffffffffffff, 0x3690000000000000, 0x3690000000000001)
			}
		}
		for vi := range values {
			var input, want [16]byte
			for lane := 0; lane < 16/inputBytes; lane++ {
				value := values[(vi+lane)%len(values)]
				if inputBytes == 8 {
					binary.LittleEndian.PutUint64(input[lane*8:], value)
				} else {
					binary.LittleEndian.PutUint32(input[lane*4:], uint32(value))
				}
				if lane < lanes {
					result := swarVectorConversionExpected(kind, value)
					if outputBytes == 8 {
						binary.LittleEndian.PutUint64(want[lane*8:], result)
					} else {
						binary.LittleEndian.PutUint32(want[lane*4:], uint32(result))
					}
				}
			}
			repeats := 1
			if inputBytes == 4 && lanes == 2 {
				repeats = 2
			}
			for ignored := 0; ignored < repeats; ignored++ {
				if ignored != 0 {
					binary.LittleEndian.PutUint64(input[8:], 0x80000000ffffffff)
				}
				copy(args, input[:])
				for n := 16; n < 32; n++ {
					results[n] = 0xa5
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:16], input[:]) {
					t.Fatal("vector saturation/precision/zero lanes", kind, vi, ignored, input, results[:16], want, err)
				}
				for n := 16; n < 32; n++ {
					if results[n] != 0xa5 {
						t.Fatal("vector conversion result tail", kind, n)
					}
				}
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("vector conversion allocations", kind, allocs)
		}
	}
}
