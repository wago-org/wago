//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64MixedIncomingAndResultSlotPacking(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	for _, pattern := range [][]byte{{0x7f, 0x7b}, {0x7b, 0x7f}, {0x7f}, {0x7b}, {0x7f, 0x7f, 0x7b, 0x7f, 0x7b, 0x7b}} {
		params := make([]byte, 128)
		body := []byte{}
		for i := range params {
			params[i] = pattern[i%len(pattern)]
			body = append(body, 0x20)
			body = uleb(body, uint32(i))
		}
		body = append(body, 0x0b)
		fn, ft := swarValidatedFunction(t, fixtureFunc{params: params, results: params, body: body})
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		want := []byte{}
		slot := 0
		for i, typ := range params {
			low := uint64(i)*0x01010101 + 0x80000000
			if typ == 0x7f {
				low |= 0xdeadbeef00000000
			}
			binary.LittleEndian.PutUint64(args[slot*8:], low)
			slot++
			if typ == 0x7f {
				low = uint64(uint32(low))
			}
			word := make([]byte, 8)
			binary.LittleEndian.PutUint64(word, low)
			want = append(want, word...)
			if typ == 0x7b {
				high := uint64(i)*0x101010101010101 + 0x7ff0000000000001
				binary.LittleEndian.PutUint64(args[slot*8:], high)
				slot++
				binary.LittleEndian.PutUint64(word, high)
				want = append(want, word...)
			}
		}
		if info.Params != slot || info.Results != slot {
			t.Fatal("mixed flattened arity", info, slot)
		}
		entry := thunkImageRV64(t, code)
		check := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:len(want)], want) {
				panic("mixed raw carrier packing")
			}
		}
		check()
		if allocs := testing.AllocsPerRun(50, check); allocs != 0 {
			t.Fatal("mixed native carrier allocations", allocs)
		}
	}
}

func TestSWARRV64TypedSelectAndAnyTrueWidth(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 6)
	for _, test := range []struct {
		typ   byte
		typed bool
	}{{0x7b, true}, {0x7f, true}, {0x7f, false}} {
		typ := test.typ
		body := []byte{0x20, 0, 0x20, 1, 0x20, 2}
		if test.typed {
			body = append(body, 0x1c, 1, typ)
		} else {
			body = append(body, 0x1b)
		}
		body = append(body, 0x0b)
		fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{typ, typ, 0x7f}, result: typ, body: body})
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		for _, cond := range []uint32{0, 1, 0x80000000, 0xffffffff} {
			var a, b []uint64
			if typ == 0x7b {
				a = []uint64{0x7ff0000000000001, 0x8000000000000000}
				b = []uint64{0xfff8000000000123, 0x1122334455667788}
			} else {
				a = []uint64{0xfeedface80000001}
				b = []uint64{0xdeadbeefffffffff}
			}
			slot := 0
			for _, values := range [][]uint64{a, b} {
				for _, v := range values {
					binary.LittleEndian.PutUint64(args[slot*8:], v)
					slot++
				}
			}
			binary.LittleEndian.PutUint64(args[slot*8:], uint64(cond)|0xf00df00d00000000)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal(err)
			}
			want := a
			if cond == 0 {
				want = b
			}
			for i, v := range want {
				if typ == 0x7f {
					v = uint64(uint32(v))
				}
				if got := binary.LittleEndian.Uint64(results[i*8:]); got != v {
					t.Fatal("select raw upper condition/value", typ, cond, got, v)
				}
			}
		}
	}
	fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{0x7b}, result: 0x7f, body: []byte{0x20, 0, 0xfd, 83, 0x0b}})
	code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, code)
	for _, pair := range [][2]uint64{{0, 0}, {0, 1}, {1, 0}, {0, 1 << 63}, {0x7ff0000000000001, 0}} {
		binary.LittleEndian.PutUint64(args, pair[0])
		binary.LittleEndian.PutUint64(args[8:], pair[1])
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		want := uint64(0)
		if pair[0] != 0 || pair[1] != 0 {
			want = 1
		}
		if got := binary.LittleEndian.Uint64(results); got != want {
			t.Fatal("any_true whole pair", pair, got, want)
		}
	}
	for _, fixture := range []fixtureFunc{
		{result: 0x7f, body: []byte{0x41, 0x7f, 0x0b}},
		{result: 0x7f, locals: []byte{1, 1, 0x7f}, body: []byte{0x20, 0, 0x0b}},
		{params: []byte{0x7f}, result: 0x7f, locals: []byte{1, 127, 0x7f}, body: []byte{0x20, 0, 0x22, 127, 0x1a, 0x20, 127, 0x0b}},
	} {
		fn, ft := swarValidatedFunction(t, fixture)
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		binary.LittleEndian.PutUint64(args, 0xa5a5a5a580000001)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		want := uint64(0xffffffff)
		if len(fn.Locals.Runs) > 0 {
			want = 0
			if len(ft.Params) > 0 {
				want = 0x80000001
			}
		}
		if got := binary.LittleEndian.Uint64(results); got != want {
			t.Fatal("i32 constant/zero/tee carrier", got, want)
		}
	}
}
