//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64ScalarSplatRawIncoming(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 2)
	for _, family := range []struct {
		kind  uint32
		width int
	}{{15, 1}, {16, 2}, {17, 4}} {
		fn, ft := swarValidatedFunction(t, swarLaneFixture(family.kind, 0))
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		for _, value := range []uint32{0, 1, 0x80, 0xff, 0x8000, 0xffff, 0x80000000, 0xffffffff, 0x12345678} {
			binary.LittleEndian.PutUint64(args, 0xfeedface00000000|uint64(value))
			want := make([]byte, 16)
			for i := range want {
				want[i] = byte(value >> (8 * (i % family.width)))
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want) {
				t.Fatal("splat raw I32", family.kind, value, results[:16], want, err)
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("splat native allocations", family.kind, allocs)
		}
	}
}

func TestSWARRV64ScalarExtractReplaceAllLanes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 3)
	rng := rand.New(rand.NewSource(64081632))
	for _, family := range []struct {
		kind    uint32
		width   int
		signed  bool
		replace bool
	}{{21, 1, true, false}, {22, 1, false, false}, {23, 1, false, true},
		{24, 2, true, false}, {25, 2, false, false}, {26, 2, false, true},
		{27, 4, false, false}, {28, 4, false, true}} {
		for lane := 0; lane < 16/family.width; lane++ {
			t.Run(fmt.Sprintf("kind%d/lane%d", family.kind, lane), func(t *testing.T) {
				fixture := swarLaneFixture(family.kind, byte(lane))
				fixture.locals = []byte{1, byte(128 - len(fixture.params)), 0x7b}
				fn, ft := swarValidatedFunction(t, fixture)
				code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
				if err != nil || info.FrameBytes <= 2048 {
					t.Fatal("large scalar lane frame", info, err)
				}
				entry := thunkImageRV64(t, code)
				for sample := 0; sample < 20; sample++ {
					var input [16]byte
					_, _ = rng.Read(input[:])
					value := rng.Uint64()
					if sample == 0 || sample == 1 {
						for i := range input {
							input[i] = byte(sample * 255)
						}
						value = 0xfeedface80000001
					} else if sample == 2 {
						binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
						binary.LittleEndian.PutUint64(input[8:], 0x8000000000000000)
						value = 0xdeadbeefffffffff
					}
					copy(args, input[:])
					binary.LittleEndian.PutUint64(args[16:], value)
					for i := range results[:16] {
						results[i] = 0xa5
					}
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
						t.Fatal(err)
					}
					start := lane * family.width
					if family.replace {
						want := input
						for i := 0; i < family.width; i++ {
							want[start+i] = byte(value >> (8 * i))
						}
						if !bytes.Equal(results[:16], want[:]) {
							t.Fatal("replace changed untouched lanes", input, value, results[:16], want)
						}
					} else {
						var want uint32
						for i := 0; i < family.width; i++ {
							want |= uint32(input[start+i]) << (8 * i)
						}
						if family.signed && input[start+family.width-1]&0x80 != 0 {
							want |= ^uint32(0) << (8 * family.width)
						}
						if got := binary.LittleEndian.Uint64(results); got != uint64(want) || !bytes.Equal(results[8:16], bytes.Repeat([]byte{0xa5}, 8)) {
							t.Fatal("extract sign/normalization/compact result", input, got, want, results[:16])
						}
					}
				}
				if lane == 0 {
					call := func() {
						if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
							panic(err)
						}
					}
					if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
						t.Fatal("scalar lane native allocations", allocs)
					}
				}
			})
		}
	}
}
