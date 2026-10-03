//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"math/bits"
	"testing"
)

func TestCallSpillPreservesRelinquishedLocalHome(t *testing.T) {
	for _, lazy := range []bool{false, true} {
		f := fn{a: &encoder.Asm{}, s: newStack(), usesCalls: lazy, pinRelinquished: true, pinnedLocals: []int{0}, localType: []machineType{mtI64}, localSlot: []uint32{0}, locals: []localDef{{typ: mtI64, reg: R12, state: lsMem}}}
		f.pushReg(R12, mtI64) // an argument owns the former local register
		f.spillLocalsForCall()
		if f.a.Len() != 0 {
			t.Fatalf("lazy=%v: overwrote the valid local home with its register's new occupant", lazy)
		}
	}
}

func TestEagerCallPreservesEightArgumentsAndLocals(t *testing.T) {
	types := make([]wasm.ValType, 8)
	for i := range types {
		types[i] = wasm.I64
	}
	body := []byte{0}
	for i := range types {
		body = append(body, 0x20, byte(i), 0x42, byte(i+17), 0x85, 0x21, byte(i))
	}
	for i := range types {
		idx := 7 - i
		if i%3 == 0 {
			idx = 0
		}
		body = append(body, 0x20, byte(idx))
		if i%2 == 1 {
			body = append(body, 0x42, byte(i+33), 0x85)
		}
	}
	body = append(body, 0x10, 1, 0x1a, 0x42, 0)
	for i := range types {
		body = append(body, 0x20, byte(i), 0x42, byte(i+1), 0x89, 0x85)
	}
	body = append(body, 0x0b)
	m := modFuncs(t, funcDef{types, []wasm.ValType{wasm.I64}, body}, funcDef{types, []wasm.ValType{wasm.I64}, []byte{0, 0x42, 0, 0x0b}})
	for _, lazy := range []bool{false, true} {
		cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"inline": false, "stack-reg": lazy}})
		if err != nil {
			t.Fatal(err)
		}
		defer cm.CodeImage.Close()
		for _, seed := range []uint64{0, 1, 0x123456789abcdef0, ^uint64(0)} {
			var args [8]uint64
			var want uint64
			for i := range args {
				args[i] = bits.RotateLeft64(seed, i) ^ uint64(i)*0x0102030405060708
				want ^= bits.RotateLeft64(args[i]^uint64(i+17), i+1)
			}
			if got := runCompiledAmd64u(t, cm, args[:]...); got != want {
				t.Fatalf("lazy=%v seed=%x got=%x want=%x", lazy, seed, got, want)
			}
		}
	}
}
