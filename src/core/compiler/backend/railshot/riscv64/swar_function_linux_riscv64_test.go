//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	goruntime "runtime"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARRV64WholeFunctionsTransportAndBitFamily(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 6)
	input := []uint64{0x7ff0000000000001, 0x8000000000000000, 0x8877665544332211, 0xffeeddccbbaa0099, 0x00ff00ff00ff00ff, 0xff00ff00ff00ff00}
	for i, v := range input {
		binary.LittleEndian.PutUint64(args[8*i:], v)
	}
	for _, op := range []byte{77, 78, 79, 80, 81, 82} {
		arity := 2
		if op == 77 {
			arity = 1
		}
		if op == 82 {
			arity = 3
		}
		params := bytes.Repeat([]byte{0x7b}, arity)
		body := []byte{}
		for i := 0; i < arity; i++ {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0xfd, op, 0x0b)
		fn, ft := swarValidatedFunction(t, fixtureFunc{params: params, result: 0x7b, body: body})
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(op, err)
		}
		for half := 0; half < 2; half++ {
			a, b, m := input[half], input[2+half], input[4+half]
			var want uint64
			switch op {
			case 77:
				want = ^a
			case 78:
				want = a & b
			case 79:
				want = a &^ b
			case 80:
				want = a | b
			case 81:
				want = a ^ b
			case 82:
				want = a&m | b&^m
			}
			if got := binary.LittleEndian.Uint64(results[half*8:]); got != want {
				t.Fatal("whole pair bit operation", op, half, got, want)
			}
		}
	}
	pressure := bytes.Repeat([]byte{0x20, 0}, 256)
	pressure = append(pressure, bytes.Repeat([]byte{0x1a}, 255)...)
	pressure = append(pressure, 0x22, 127, 0x1a, 0x20, 127, 0x0b)
	for _, fixture := range []fixtureFunc{
		{params: []byte{0x7b}, result: 0x7b, locals: []byte{1, 127, 0x7b}, body: pressure},
		{result: 0x7b, locals: []byte{1, 128, 1, 0x7b}, body: []byte{0x20, 127, 0x0b}},
		{result: 0x7b, body: append(swarLiteral(input[0], input[1]), 0x0b)},
	} {
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true, entryPC: 73, functionIndex: 11})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(info, err)
		}
		want := args[:16]
		if len(ft.Params) == 0 && len(fn.Locals.Runs) > 0 {
			want = make([]byte, 16)
		}
		if !bytes.Equal(results[:16], want) {
			t.Fatal("whole pair raw/zero/largehome", info, results[:16], want)
		}
		// Measure the actual foreign bridge SP through a small framed probe;
		// do not hard-code the wrapper save-frame size or reject in that bridge.
		var report enc.Asm
		report.Ld(enc.Scratch1, enc.SP, 8)
		report.Sd(enc.SP, enc.Scratch1, 0)
		probeCode, probeInfo, err := frameFunction(report.B, nil, 0, 0, false, -1, FunctionInfo{mode: emissionMode{production: true}})
		if err != nil {
			t.Fatal(err)
		}
		probeEntry := thunkImageRV64(t, probeCode)
		if err := e.CallPrepared(probeEntry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		bridgeSP := binary.LittleEndian.Uint64(results) + uint64(probeInfo.FrameBytes)
		j.SetStackFence(uintptr(bridgeSP - uint64(info.FrameBytes) + 16))
		for i := range results {
			results[i] = 0xa5
		}
		var bounds *coreruntime.TrapError
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); !errors.As(err, &bounds) || bounds.Code != coreruntime.TrapStackFenceBreached || binary.LittleEndian.Uint64(trap[16:]) != uint64(73)<<32|12 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("whole pair stack prewrite trap", err)
		}
		j.SetStackFence(e.StackLimit())
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal("whole pair stack reuse", err)
		}
	}
}

func TestSWARRV64WholeFunctionMaximumResults(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	fixture := fixtureFunc{params: []byte{0x7b}, results: bytes.Repeat([]byte{0x7b}, 128), body: append(bytes.Repeat([]byte{0x20, 0}, 128), 0x0b)}
	fn, ft := swarValidatedFunction(t, fixture)
	code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.Results != 256 {
		t.Fatal("pair result slots", info)
	}
	entry := thunkImageRV64(t, code)
	binary.LittleEndian.PutUint64(args, 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(args[8:], 0x8000000000000000)
	check := func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
		for i := 0; i < 128; i++ {
			if !bytes.Equal(results[i*16:i*16+16], args[:16]) {
				panic("pair result slot")
			}
		}
	}
	check()
	if allocs := testing.AllocsPerRun(50, check); allocs != 0 {
		t.Fatal("pair native call allocation", allocs)
	}
}

func TestSWARRV64WholeFunctionMaximumIncomingParameters(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	fixture := fixtureFunc{params: bytes.Repeat([]byte{0x7b}, 128), result: 0x7b, body: []byte{0x20, 127, 0x0b}}
	fn, ft := swarValidatedFunction(t, fixture)
	code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.Params != 256 {
		t.Fatal("pair incoming slots", info)
	}
	for i := 0; i < 256; i++ {
		binary.LittleEndian.PutUint64(args[i*8:], uint64(i)*0x0101010101010101)
	}
	binary.LittleEndian.PutUint64(args[254*8:], 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(args[255*8:], 0x8000000000000000)
	entry := thunkImageRV64(t, code)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], args[254*8:256*8]) {
		t.Fatal("last incoming pair", info, err)
	}
}
