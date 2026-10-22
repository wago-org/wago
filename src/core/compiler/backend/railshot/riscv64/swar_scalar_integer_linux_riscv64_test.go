//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	goruntime "runtime"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARRV64ScalarIntegerDirtyInputsLiveVectorsAndTraps(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 6)
	for _, c := range swarScalarIntegerCases() {
		fixture, opOffset := swarScalarIntegerFixture(c)
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true, functionIndex: 9})
		if err != nil || info.FrameBytes <= 2048 || info.Params != len(fixture.params)+1 || info.Results != 5 {
			t.Fatal("mixed integer frame", c, info, err)
		}
		entry := thunkImageRV64(t, code)
		check := func(a, b uint64) {
			var input [32]byte
			binary.LittleEndian.PutUint64(input[:], 0x7ff0000000000001)
			binary.LittleEndian.PutUint64(input[8:], 0x80000000deadbeef)
			binary.LittleEndian.PutUint64(input[16:], a)
			binary.LittleEndian.PutUint64(input[24:], b)
			copy(args, input[:])
			for i := range results {
				results[i] = 0xa5
			}
			want, trapCode := swarScalarIntegerExpected(c, a, b)
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			if !bytes.Equal(args[:32], input[:]) {
				t.Fatal("integer changed input cells", c, a, b)
			}
			if trapCode != 0 {
				var tr *coreruntime.TrapError
				pc := uint32(fn.LocalDeclBytes) + uint32(opOffset)
				if !errors.As(err, &tr) || tr.Code != coreruntime.TrapCode(productionTrapCode(trapCode)) || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|10 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
					t.Fatal("integer trap source/prewrite", c, a, b, err, trap[16:24])
				}
				return
			}
			var expected [40]byte
			copy(expected[:16], input[:16])
			binary.LittleEndian.PutUint64(expected[16:], want)
			if c.resultWide {
				binary.LittleEndian.PutUint64(expected[24:], want)
				binary.LittleEndian.PutUint64(expected[32:], want)
			} else {
				for lane := 0; lane < 4; lane++ {
					binary.LittleEndian.PutUint32(expected[24+4*lane:], uint32(want))
				}
			}
			if err != nil || !bytes.Equal(results[:40], expected[:]) {
				t.Fatal("integer/live vector/local/splat", c, a, b, results[:40], expected, err)
			}
			for i := 40; i < 48; i++ {
				if results[i] != 0xa5 {
					t.Fatal("integer result tail", c, i)
				}
			}
		}
		for _, pair := range dirtyInputs() {
			check(pair[0], pair[1])
		}
		// Trap cells, the large canonical frame and result homes must be reusable.
		check(7, 3)
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("mixed scalar integer allocations", c, allocs)
		}
	}
}
