//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64FloatingVectorBasicMathAndRawSign(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, kind := range swarFloatMathKinds {
		width, operation, unary, _ := swarFloatMathOpcode(kind)
		wide := width == 64
		laneBytes := int(width) / 8
		fixture := swarArithmeticFixture(kind)
		if unary {
			fixture = swarUnaryFixture(kind, false)
		}
		fixture.locals = []byte{1, byte(128 - len(fixture.params)), 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large FP math frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		values := floatInputs(wide)
		// Add precise half-ULP partners for round-to-even and cancellation cases.
		if wide {
			values = append(values, 0x3ca0000000000000, 0xbca0000000000000, 0x3ff0000000000001)
		} else {
			values = append(values, 0x33800000, 0xb3800000, 0x3f800001)
		}
		n := byte(operation)
		if operation >= swarFloatSqrt {
			n += 4
		}
		partners := len(values)
		if unary {
			partners = 1
		}
		for ai := range values {
			for bi := 0; bi < partners; bi++ {
				var input [32]byte
				var want [16]byte
				for lane := 0; lane < 16/laneBytes; lane++ {
					a, b := values[(ai+lane)%len(values)], values[(bi+2*lane)%len(values)]
					value := floatMathExpected(n, a, b, wide)
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
				copy(args, input[:])
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
					t.Fatal("FP vector rounding/NaN/rawsign", kind, ai, bi, input, results[:16], want, err)
				}
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("FP math allocations", kind, allocs)
		}
	}
}
