//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64FloatingComparisonsAllMasksAndIEEEInputs(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, wide := range []bool{false, true} {
		base, width := uint32(65), 4
		if wide {
			base, width = 71, 8
		}
		values := floatInputs(wide)
		for relation := 0; relation < 6; relation++ {
			kind := base + uint32(relation)
			fixture := swarArithmeticFixture(kind)
			fixture.locals = []byte{1, 126, 0x7b}
			fn, ft := swarValidatedFunction(t, fixture)
			code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
			if err != nil || info.FrameBytes <= 2048 {
				t.Fatal("large FP compare frame", kind, info, err)
			}
			entry := thunkImageRV64(t, code)
			for ai := range values {
				for bi := range values {
					var input [32]byte
					var want [16]byte
					for lane := 0; lane < 16/width; lane++ {
						a, b := values[(ai+lane)%len(values)], values[(bi+2*lane)%len(values)]
						if wide {
							binary.LittleEndian.PutUint64(input[lane*8:], a)
							binary.LittleEndian.PutUint64(input[16+lane*8:], b)
						} else {
							binary.LittleEndian.PutUint32(input[lane*4:], uint32(a))
							binary.LittleEndian.PutUint32(input[16+lane*4:], uint32(b))
						}
						if swarFloatComparisonOracle(a, b, width*8, relation) {
							for i := 0; i < width; i++ {
								want[lane*width+i] = 255
							}
						}
					}
					copy(args, input[:])
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
						t.Fatal("FP unordered/zero/complete masks", kind, ai, bi, input, results[:16], want, err)
					}
				}
			}
			call := func() {
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}
			if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
				t.Fatal("FP comparison allocations", kind, allocs)
			}
		}
	}
}
