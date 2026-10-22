//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64WideAndFloatLaneRawTransport(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 3)
	rng := rand.New(rand.NewSource(641834))
	for _, kind := range []uint32{18, 19, 20, 29, 30, 31, 32, 33, 34} {
		width, lanes := 8, 1
		if kind == 19 || kind == 31 || kind == 32 {
			width = 4
		}
		if kind >= 29 {
			lanes = 16 / width
		}
		for lane := 0; lane < lanes; lane++ {
			fixture := swarWideLaneFixture(kind, byte(lane))
			fixture.locals = []byte{1, byte(128 - len(fixture.params)), 0x7b}
			fn, ft := swarValidatedFunction(t, fixture)
			code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
			if err != nil || info.FrameBytes <= 2048 {
				t.Fatal("wide/FP lane frame", kind, info, err)
			}
			entry := thunkImageRV64(t, code)
			for sample := 0; sample < 32; sample++ {
				var input [24]byte
				_, _ = rng.Read(input[:16])
				values := []uint64{0, 1, 0xffffffffffffffff, 0x8000000000000000, 0x7ff0000000000001, 0xfff8000000000123, 0x7fa00001, 0x80000000, 0xffffffff}
				value := rng.Uint64()
				if sample < len(values) {
					value = values[sample]
					binary.LittleEndian.PutUint64(input[:8], 0x7ff000007fa00001)
					binary.LittleEndian.PutUint64(input[8:16], 0x8000000080000000)
				}
				if width == 4 {
					value |= 0xfeedface00000000
				}
				var want [16]byte
				outputBytes := 16
				if kind <= 20 {
					binary.LittleEndian.PutUint64(input[:8], value)
					for i := range want {
						want[i] = byte(value >> (8 * (i % width)))
					}
				} else if kind == 30 || kind == 32 || kind == 34 {
					binary.LittleEndian.PutUint64(input[16:], value)
					copy(want[:], input[:16])
					for i := 0; i < width; i++ {
						want[lane*width+i] = byte(value >> (8 * i))
					}
				} else {
					outputBytes = 8
					copy(want[:width], input[lane*width:(lane+1)*width])
				}
				copy(args, input[:])
				for i := range results[:16] {
					results[i] = 0xa5
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:outputBytes], want[:outputBytes]) || !bytes.Equal(args[:24], input[:]) {
					t.Fatal("wide/FP raw bits", kind, lane, value, input, results[:outputBytes], want, err)
				}
				if outputBytes == 8 && !bytes.Equal(results[8:16], bytes.Repeat([]byte{0xa5}, 8)) {
					t.Fatal("wide/FP scalar result overwrote following cell", kind)
				}
			}
			call := func() {
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}
			if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
				t.Fatal("wide/FP lane native allocations", kind, allocs)
			}
		}
	}
}

func TestSWARRV64WideFloatConstantsLocalsAndSelect(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 3)
	for _, typ := range []byte{0x7e, 0x7d, 0x7c} {
		for _, value := range []uint64{0, 0x8000000000000000, 0x7ff0000000000001, 0xfff8000000000123, 0x7fa00001, 0x80000000, 0xffffffffffffffff} {
			fn, ft := swarValidatedFunction(t, swarScalarConstantFixture(typ, value))
			code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
			if err != nil {
				t.Fatal(err)
			}
			entry := thunkImageRV64(t, code)
			want := value
			if typ == 0x7d {
				want = uint64(uint32(value))
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != want {
				t.Fatal("raw scalar constant", typ, value, results[:8], err)
			}
		}
		for _, fixture := range []fixtureFunc{
			{result: typ, locals: []byte{1, 1, typ}, body: []byte{0x20, 0, 0x0b}},
			{params: []byte{typ}, result: typ, locals: []byte{1, 127, typ}, body: []byte{0x20, 0, 0x22, 127, 0x1a, 0x20, 127, 0x0b}},
		} {
			fn, ft := swarValidatedFunction(t, fixture)
			code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
			if err != nil {
				t.Fatal(err)
			}
			entry := thunkImageRV64(t, code)
			value := uint64(0x7ff000007fa00001)
			binary.LittleEndian.PutUint64(args, value)
			want := uint64(0)
			if len(fixture.params) > 0 {
				want = value
				if typ == 0x7d {
					want = uint64(uint32(value))
				}
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != want {
				t.Fatal("raw scalar zero/tee local", typ, results[:8], want, err)
			}
		}
		for _, typed := range []bool{false, true} {
			body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x1b}
			if typed {
				body[len(body)-1] = 0x1c
				body = append(body, 1, typ)
			}
			body = append(body, 0x0b)
			fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{typ, typ, 0x7f}, result: typ, body: body})
			code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
			if err != nil {
				t.Fatal(err)
			}
			entry := thunkImageRV64(t, code)
			left, right := uint64(0x7ff000007fa00001), uint64(0x8000000080000000)
			for _, cond := range []uint32{0, 1, 0x80000000, 0xffffffff} {
				binary.LittleEndian.PutUint64(args, left)
				binary.LittleEndian.PutUint64(args[8:], right)
				binary.LittleEndian.PutUint64(args[16:], uint64(cond)|0xfeedface00000000)
				want := left
				if cond == 0 {
					want = right
				}
				if typ == 0x7d {
					want = uint64(uint32(want))
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != want {
					t.Fatal("raw scalar typed/untyped select", typ, typed, cond, results[:8], want, err)
				}
			}
		}
	}
}
