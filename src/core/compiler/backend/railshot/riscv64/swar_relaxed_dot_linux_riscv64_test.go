//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64RelaxedDotSaturatedPairsAndWrappedAccumulators(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 6)
	rng := rand.New(rand.NewSource(64274275))
	for _, kind := range []uint32{274, 275} {
		fixture := swarRelaxedFixture(kind)
		fixture.locals = []byte{1, byte(128 - len(fixture.params)), 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large relaxed dot frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		check := func(input [48]byte, label int) {
			want := swarRelaxedDotOracle(input[:], kind == 275)
			copy(args, input[:])
			for i := 16; i < 32; i++ {
				results[i] = 0xa5
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:48], input[:]) {
				t.Fatal("relaxed signed dot/saturated intermediate/wrap", kind, label, input, results[:16], want, err)
			}
			for i := 16; i < 32; i++ {
				if results[i] != 0xa5 {
					t.Fatal("relaxed dot result tail", kind, label, i)
				}
			}
		}
		for sample := 0; sample < 128; sample++ {
			var input [48]byte
			_, _ = rng.Read(input[:])
			if sample < 8 {
				edges := []byte{0x80, 0x7f, 0, 1, 0xff, 0x40, 0xc0, 0x81}
				for lane := 0; lane < 16; lane++ {
					input[lane], input[16+lane] = edges[sample], edges[(sample/4+lane)%len(edges)]
				}
			} else if sample == 8 || sample == 9 {
				for lane := 0; lane < 16; lane++ {
					input[lane], input[16+lane] = 0x80, 0x80
					if sample == 9 && lane%4 == 2 {
						input[16+lane] = 0x7f
					}
				}
			}
			check(input, sample)
		}
		// Every signed-byte product pair, duplicated into adjacent lanes, checks
		// positive overflow, negative products and full-width wrap with sentinels.
		for a := 0; a < 256; a++ {
			for b := 0; b < 256; b++ {
				var input [48]byte
				for lane := 0; lane < 16; lane++ {
					input[lane], input[16+lane] = byte(a), byte(b)
				}
				for lane, accumulator := range []uint32{0, 0xffffffff, 0x7fffffff, 0x80000000} {
					binary.LittleEndian.PutUint32(input[32+4*lane:], accumulator)
				}
				check(input, 256*a+b)
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("relaxed dot allocations", kind, allocs)
		}
	}
}
