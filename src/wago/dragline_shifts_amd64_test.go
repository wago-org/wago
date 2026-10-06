//go:build amd64 && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDraglineAMD64VariableShiftsPreserveOperands(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   wasm.ValType
		op    byte
		shift func(uint64, uint64) uint64
	}{
		{"i32.shl", wasm.I32, 0x74, func(x, n uint64) uint64 { return uint64(uint32(x) << uint32(n&31)) }},
		{"i32.shr_s", wasm.I32, 0x75, func(x, n uint64) uint64 { return uint64(uint32(int32(x) >> uint32(n&31))) }},
		{"i32.shr_u", wasm.I32, 0x76, func(x, n uint64) uint64 { return uint64(uint32(x) >> uint32(n&31)) }},
		{"i64.shl", wasm.I64, 0x86, func(x, n uint64) uint64 { return x << (n & 63) }},
		{"i64.shr_s", wasm.I64, 0x87, func(x, n uint64) uint64 { return uint64(int64(x) >> (n & 63)) }},
		{"i64.shr_u", wasm.I64, 0x88, func(x, n uint64) uint64 { return x >> (n & 63) }},
	} {
		for _, target := range []CompilerTargetMode{TargetCompatibility, TargetNative} {
			t.Run(fmt.Sprintf("%s/%v", tc.name, target), func(t *testing.T) {
				xor := byte(0x73)
				if tc.typ == wasm.I64 {
					xor = 0x85
				}
				source := draglineBinaryModule(tc.typ, tc.typ, []byte{0x20, 0, 0x20, 1, tc.op, 0x20, 0, xor, 0x20, 1, xor, 0x0b})
				compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(target), source)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				check := func(x, n uint64) {
					t.Helper()
					want := tc.shift(x, n) ^ x ^ n
					if tc.typ == wasm.I32 {
						want = uint64(uint32(want))
						x = uint64(uint32(x))
						n = uint64(uint32(n))
					}
					got, err := instance.Invoke("run", x, n)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("run(%#x,%#x)=%x,%v; want %#x", x, n, got, err, want)
					}
				}
				for _, x := range []uint64{0, 1, 0x80000000, 0x8000000000000000, ^uint64(0), 0x123456789abcdef0} {
					for _, n := range []uint64{0, 1, 2, 31, 32, 33, 63, 64, 65, 127, 255, ^uint64(0)} {
						check(x, n)
					}
				}
				state := uint64(0x9e3779b97f4a7c15)
				for range 512 {
					state ^= state << 13
					state ^= state >> 7
					state ^= state << 17
					check(state, state>>32)
				}
			})
		}
	}
}
