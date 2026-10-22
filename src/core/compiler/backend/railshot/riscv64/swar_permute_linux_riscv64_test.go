//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64ByteShuffleEverySourceAndDestination(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	rng := rand.New(rand.NewSource(641332))
	for pattern := 0; pattern < 36; pattern++ {
		var indices [16]byte
		for lane := range indices {
			indices[lane] = byte((pattern + lane) % 32)
			if pattern == 32 {
				indices[lane] = byte(31 - lane)
			} else if pattern == 33 {
				indices[lane] = byte(lane * 2)
			} else if pattern == 34 {
				indices[lane] = 0
			} else if pattern == 35 {
				indices[lane] = 31
			}
		}
		fixture := swarPermutationFixture(true, indices)
		fixture.locals = []byte{1, 126, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large shuffle frame", info, err)
		}
		entry := thunkImageRV64(t, code)
		for sample := 0; sample < 8; sample++ {
			var input [32]byte
			_, _ = rng.Read(input[:])
			if sample == 0 {
				for i := range input {
					input[i] = byte(i + 0x31)
				}
			} else if sample == 1 {
				binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(input[8:16], 0x8000000000000000)
				binary.LittleEndian.PutUint64(input[16:24], 0xfff8000000000123)
				binary.LittleEndian.PutUint64(input[24:], 0x8877665544332211)
			}
			var want [16]byte
			for lane, index := range indices {
				want[lane] = input[index]
			}
			copy(args, input[:])
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
				t.Fatal("shuffle byte selection", indices, input, results[:16], want, err)
			}
		}
		if pattern == 0 {
			call := func() {
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}
			if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
				t.Fatal("shuffle native allocations", allocs)
			}
		}
	}
}

func TestSWARRV64ByteSwizzleAllRuntimeIndices(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	fixture := swarPermutationFixture(false, [16]byte{})
	fixture.locals = []byte{1, 126, 0x7b}
	fn, ft := swarValidatedFunction(t, fixture)
	code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
	if err != nil || info.FrameBytes <= 2048 {
		t.Fatal("large swizzle frame", info, err)
	}
	entry := thunkImageRV64(t, code)
	rng := rand.New(rand.NewSource(6414256))
	for base := 0; base < 256; base++ {
		for sample := 0; sample < 8; sample++ {
			var input [32]byte
			_, _ = rng.Read(input[:16])
			if sample == 0 {
				for i := 0; i < 16; i++ {
					input[i] = byte(0x41 + i*7)
				}
			} else if sample == 1 {
				binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(input[8:16], 0x8000000000000000)
			}
			var want [16]byte
			for lane := 0; lane < 16; lane++ {
				index := byte(base + lane)
				input[16+lane] = index
				if index < 16 {
					want[lane] = input[index]
				}
			}
			copy(args, input[:])
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
				t.Fatal("swizzle valid/invalid runtime byte", base, input, results[:16], want, err)
			}
		}
	}
	call := func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}
	if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
		t.Fatal("swizzle native allocations", allocs)
	}
}
