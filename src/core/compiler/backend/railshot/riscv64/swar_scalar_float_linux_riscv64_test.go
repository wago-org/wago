//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	goruntime "runtime"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarScalarFloatExpected(c swarScalarFloatCase, a, b uint64) (uint64, bool) {
	inputWide, outputWide := c.input == 0x7e || c.input == 0x7c, c.output == 0x7e || c.output == 0x7c
	if !inputWide {
		a, b = uint64(uint32(a)), uint64(uint32(b))
	}
	if c.op == 0xfc || c.trapping {
		unsigned := c.op&1 != 0
		if c.op == 0xfc {
			unsigned = c.sub&1 != 0
		}
		return truncExpected(floatValue(a, inputWide), outputWide, unsigned, c.op == 0xfc)
	}
	if c.op >= 0x5b && c.op <= 0x66 {
		n := c.op - 0x5b
		if inputWide {
			n = c.op - 0x61
		}
		x, y := floatValue(a, inputWide), floatValue(b, inputWide)
		if [...]bool{x == y, x != y, x < y, x > y, x <= y, x >= y}[n] {
			return 1, false
		}
		return 0, false
	}
	if c.op >= 0x8b && c.op <= 0xa6 {
		n := c.op - 0x8b
		if inputWide {
			n = c.op - 0x99
		}
		return floatMathExpected(n, a, b, inputWide), false
	}
	var result uint64
	switch c.op {
	case 0xb2:
		result = uint64(math.Float32bits(float32(int32(a))))
	case 0xb3:
		result = uint64(math.Float32bits(float32(uint32(a))))
	case 0xb4:
		result = uint64(math.Float32bits(float32(int64(a))))
	case 0xb5:
		result = uint64(math.Float32bits(float32(a)))
	case 0xb6, 0xbb:
		result = floatBits(floatValue(a, inputWide), outputWide)
		if math.IsNaN(floatValue(a, inputWide)) {
			result = 0x7fc00000
			if outputWide {
				result = 0x7ff8000000000000
			}
		}
	case 0xb7:
		result = math.Float64bits(float64(int32(a)))
	case 0xb8:
		result = math.Float64bits(float64(uint32(a)))
	case 0xb9:
		result = math.Float64bits(float64(int64(a)))
	case 0xba:
		result = math.Float64bits(float64(a))
	default:
		result = a
	}
	return result, false
}

func TestSWARRV64ScalarFloatCompositionRawBitsRoundingAndTraps(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 6)
	for _, c := range swarScalarFloatCases() {
		fixture, opOffset := swarScalarFloatFixture(c)
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true, functionIndex: 11})
		if err != nil || info.FrameBytes <= 2048 || info.Results != 5 {
			t.Fatal("mixed FP frame", c, info, err)
		}
		entry := thunkImageRV64(t, code)
		wide := c.input == 0x7e || c.input == 0x7c
		values := []uint64{0, 1, 0xffffffff, 0x80000000, 0x7fffffff, 0x7f800001, 0x7ff0000000000001, 0x1000001, 0x1000003, 0x20000000000001, 0x7fffffffffffffff, 0x8000000000000000, ^uint64(0)}
		if c.input == 0x7d || c.input == 0x7c {
			values = floatInputs(wide)
			for _, value := range []float64{-0.999, -1.001, -2147483648.75, 2147483647.75, 4294967295.75, -2147483649, 4294967296} {
				values = append(values, floatBits(value, wide))
			}
			for _, edge := range []float64{-2147483648, 2147483648, 4294967296, -0x1p63, 0x1p63, 0x1p64} {
				if wide {
					values = append(values, floatBits(math.Nextafter(edge, math.Inf(-1)), true), floatBits(edge, true), floatBits(math.Nextafter(edge, math.Inf(1)), true))
				} else {
					values = append(values, uint64(math.Float32bits(math.Nextafter32(float32(edge), float32(math.Inf(-1))))), floatBits(edge, false), uint64(math.Float32bits(math.Nextafter32(float32(edge), float32(math.Inf(1))))))
				}
			}
			if wide {
				values = append(values, 0x3ca0000000000000, 0xbca0000000000000, 0x3ff000000fffffff, 0x3ff0000010000000, 0x3ff0000010000001, 0x3690000000000000)
			} else {
				values = append(values, 0x33800000, 0xb3800000, 0x3f800001)
			}
		}
		check := func(a, b uint64) {
			var input [32]byte
			binary.LittleEndian.PutUint64(input[:], 0x7ff0000000000001)
			binary.LittleEndian.PutUint64(input[8:], 0x80000000deadbeef)
			if !wide {
				a, b = dirty(uint32(a)), dirty(uint32(b))
			}
			binary.LittleEndian.PutUint64(input[16:], a)
			binary.LittleEndian.PutUint64(input[24:], b)
			copy(args, input[:])
			for i := range results {
				results[i] = 0xa5
			}
			want, trapping := swarScalarFloatExpected(c, a, b)
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			if !bytes.Equal(args[:32], input[:]) {
				t.Fatal("FP changed input cells", c, a, b)
			}
			if trapping {
				var tr *coreruntime.TrapError
				pc := uint32(fn.LocalDeclBytes) + uint32(opOffset)
				if !errors.As(err, &tr) || tr.Code != coreruntime.TrapTruncOverflow || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|12 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
					t.Fatal("FP trap source/prewrite", c, a, b, err, trap[16:24])
				}
				return
			}
			var expected [40]byte
			copy(expected[:16], input[:16])
			binary.LittleEndian.PutUint64(expected[16:], want)
			if c.output == 0x7e || c.output == 0x7c {
				binary.LittleEndian.PutUint64(expected[24:], want)
				binary.LittleEndian.PutUint64(expected[32:], want)
			} else {
				for lane := 0; lane < 4; lane++ {
					binary.LittleEndian.PutUint32(expected[24+4*lane:], uint32(want))
				}
			}
			if err != nil || !bytes.Equal(results[:40], expected[:]) {
				t.Fatal("scalar FP/live vector/local/splat", c, a, b, results[:40], expected, err)
			}
			for i := 40; i < 48; i++ {
				if results[i] != 0xa5 {
					t.Fatal("scalar FP result tail", c, i)
				}
			}
		}
		partners := len(values)
		if c.unary {
			partners = 1
		}
		for _, a := range values {
			for _, b := range values[:partners] {
				check(a, b)
			}
		}
		// Successful zero conversion after a trap exercises the same frame,
		// FCSR restoration and trap/result cells before allocation measurement.
		check(0, 0)
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("mixed scalar FP allocations", c, allocs)
		}
	}
}
