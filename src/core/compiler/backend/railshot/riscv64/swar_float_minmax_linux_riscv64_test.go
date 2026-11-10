//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64FloatingVectorMinMaxRawPseudoPayloads(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, kind := range swarFloatMinMaxKinds {
		width, maximum, pseudo, _ := swarFloatMinMaxOpcode(kind)
		laneBytes := int(width) / 8
		fixture := swarArithmeticFixture(kind)
		fixture.locals = []byte{1, 126, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large FP minmax frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		values := floatInputs(width == 64)
		for ai := range values {
			for bi := range values {
				var input [32]byte
				var want [16]byte
				for lane := 0; lane < 16/laneBytes; lane++ {
					a, b := values[(ai+lane)%len(values)], values[(bi+2*lane)%len(values)]
					value := swarFloatMinMaxOracle(a, b, width, maximum, pseudo)
					if width == 64 {
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
				for i := 16; i < 32; i++ {
					results[i] = 0xa5
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
					t.Fatal("FP vector minmax NaN/zero/raw pseudo", kind, ai, bi, input, results[:16], want, err)
				}
				for i := 16; i < 32; i++ {
					if results[i] != 0xa5 {
						t.Fatal("FP minmax result tail", kind, i)
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
			t.Fatal("FP minmax allocations", kind, allocs)
		}
	}
}
