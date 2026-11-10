//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64RelaxedMultiplyAddUnfusedIEEETriples(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 6)
	for kind := uint32(261); kind <= 264; kind++ {
		wide, negative := kind >= 263, kind == 262 || kind == 264
		laneBytes, sign := 4, uint64(0x80000000)
		if wide {
			laneBytes, sign = 8, 0x8000000000000000
		}
		fixture := swarRelaxedFixture(kind)
		fixture.locals = []byte{1, 125, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large relaxed madd frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		values := floatInputs(wide)
		// With c=-1, (1+u)*(1-u) distinguishes rounded multiplication
		// followed by addition from fused addition at both lane widths.
		if wide {
			values = append(values, 0x3ff0000000000001, 0x3feffffffffffffe)
		} else {
			values = append(values, 0x3f800001, 0x3f7ffffe)
		}
		for ai := range values {
			for bi := range values {
				for ci := range values {
					var input [48]byte
					var want [16]byte
					for lane := 0; lane < 16/laneBytes; lane++ {
						a, b, c := values[(ai+lane)%len(values)], values[(bi+2*lane)%len(values)], values[(ci+3*lane)%len(values)]
						left := a
						if negative {
							left ^= sign
						}
						product := floatMathExpected(9, left, b, wide)
						value := floatMathExpected(7, product, c, wide)
						if wide {
							binary.LittleEndian.PutUint64(input[lane*8:], a)
							binary.LittleEndian.PutUint64(input[16+lane*8:], b)
							binary.LittleEndian.PutUint64(input[32+lane*8:], c)
							binary.LittleEndian.PutUint64(want[lane*8:], value)
						} else {
							binary.LittleEndian.PutUint32(input[lane*4:], uint32(a))
							binary.LittleEndian.PutUint32(input[16+lane*4:], uint32(b))
							binary.LittleEndian.PutUint32(input[32+lane*4:], uint32(c))
							binary.LittleEndian.PutUint32(want[lane*4:], uint32(value))
						}
					}
					copy(args, input[:])
					for i := 16; i < 32; i++ {
						results[i] = 0xa5
					}
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:48], input[:]) {
						t.Fatal("relaxed unfused IEEE triple", kind, ai, bi, ci, input, results[:16], want, err)
					}
					for i := 16; i < 32; i++ {
						if results[i] != 0xa5 {
							t.Fatal("relaxed madd result tail", kind, i)
						}
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
			t.Fatal("relaxed madd allocations", kind, allocs)
		}
	}
}
