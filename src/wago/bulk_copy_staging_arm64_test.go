//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBulkCopyStagingPreservesPrefixAndTraps(t *testing.T) {
	body := []byte{
		0x42, 42, // i64 value remains below the copy operands
		0x41, 0x90, 0x03, 0x41, 0xda, 0x00, 0x3a, 0, 0, // store8(400, 0x5a)
		0x20, 0, 0x41, 1, 0x6a, // dst + 1, wrapping i32
		0x20, 1, 0x41, 2, 0x6a, // src + 2, wrapping i32
		0x20, 2, 0xfc, 10, 0, 0, // memory.copy
		0x42, 1, 0x7c, 0x0b, // return prefix + 1
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, bounds := range modes {
		c, err := NewRuntimeConfig().WithBoundsChecks(bounds).Compile(module)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		mem, err := in.ExportedMemory("memory")
		if err != nil {
			t.Fatal(err)
		}
		initial := make([]byte, 65536)
		for i := range initial {
			initial[i] = byte(i*29 + i>>8)
		}
		for _, n := range []uint32{0, 1, 7, 8, 15, 31, 32, 64, 4096} {
			for _, pair := range [][2]uint32{{0, 0}, {101, 100}, {100, 101}, {65535, 0}, {0, 65534}, {0xffffffff, 0xfffffffe}} {
				copy(mem.UnsafeBytes(), initial)
				want := bytes.Clone(initial)
				want[400] = 0x5a
				dst, src := uint64(pair[0]+1), uint64(pair[1]+2)
				valid := dst+uint64(n) <= 65536 && src+uint64(n) <= 65536
				if valid {
					copy(want[dst:dst+uint64(n)], want[src:src+uint64(n)])
				}
				got, err := in.Invoke("copy", uint64(pair[0])|0xaabbccdd00000000, uint64(pair[1])|0xaabbccdd00000000, uint64(n)|0xaabbccdd00000000)
				var trap *TrapError
				if valid && (err != nil || len(got) != 1 || got[0] != 43) || !valid && (!errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds) || !bytes.Equal(mem.UnsafeBytes(), want) {
					t.Fatalf("bounds=%v dst=%x src=%x n=%d valid=%t result=%v error=%v", bounds, pair[0], pair[1], n, valid, got, err)
				}
			}
		}
		in.Close()
		c.Close()
	}
}

func TestBulkCopyStagingPreservesPendingLoad(t *testing.T) {
	body := []byte{
		0x41, 0xe4, 0x00, 0x2d, 0, 0, // load8_u(100), retained below copy args
		0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 0, 0, 0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	mem, err := in.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	mem.UnsafeBytes()[100], mem.UnsafeBytes()[200] = 0x11, 0x22
	got, err := in.Invoke("copy", 100, 200, 1)
	if err != nil || len(got) != 1 || got[0] != 0x11 || mem.UnsafeBytes()[100] != 0x22 {
		t.Fatalf("result=%v error=%v memory[100]=%x", got, err, mem.UnsafeBytes()[100])
	}
}
