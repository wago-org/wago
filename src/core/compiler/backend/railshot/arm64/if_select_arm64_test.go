//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAffineIfSelectPreservesStackValueArm64(t *testing.T) {
	i32x3 := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	i32 := []wasm.ValType{wasm.I32}
	// local.get 2; local.get 0; if (result i32) { 0 } else {
	// local.get 1; i32.const 2; i32.add; i32.const 8; i32.mul
	// }; i32.add
	body := []byte{
		0x00,
		0x20, 0x02, 0x20, 0x00, 0x04, 0x7f,
		0x41, 0x00,
		0x05,
		0x20, 0x01, 0x41, 0x02, 0x6a, 0x41, 0x08, 0x6c,
		0x0b, 0x6a, 0x0b,
	}
	m := mod1(t, i32x3, i32, body)
	stats := &ModuleStats{}
	for _, tc := range []struct {
		cond, x, base uint64
		want          uint32
	}{
		{cond: 1, x: 7, base: 100, want: 100},
		{cond: 0, x: 7, base: 100, want: 172},
	} {
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Stats: stats}, tc.cond, tc.x, tc.base)
		if err != nil || uint32(got) != tc.want {
			t.Fatalf("cond=%d x=%d base=%d: got=%d err=%v want=%d", tc.cond, tc.x, tc.base, got, err, tc.want)
		}
	}
	if got := stats.Funcs[0].Peephole["if-affine-select"]; got != 1 {
		t.Fatalf("if-affine-select hits = %d, want 1", got)
	}
}

func TestAffineIfSelectReusesComparedIncrementArm64(t *testing.T) {
	i32x3 := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	i32 := []wasm.ValType{wasm.I32}
	// local.get 2; (local.get 0 + 1 == local.get 1); if (result i32) {
	// 0 } else { (local.get 0 + 2) * 8 }; i32.add
	body := []byte{
		0x00,
		0x20, 0x02,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x20, 0x01, 0x46,
		0x04, 0x7f, 0x41, 0x00, 0x05,
		0x20, 0x00, 0x41, 0x02, 0x6a, 0x41, 0x08, 0x6c,
		0x0b, 0x6a, 0x0b,
	}
	m := mod1(t, i32x3, i32, body)
	stats := &ModuleStats{}
	for _, tc := range []struct {
		i, n, base uint64
		want       uint32
	}{
		{i: 7, n: 8, base: 100, want: 100},
		{i: 7, n: 20, base: 100, want: 172},
	} {
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Stats: stats}, tc.i, tc.n, tc.base)
		if err != nil || uint32(got) != tc.want {
			t.Fatalf("i=%d n=%d base=%d: got=%d err=%v want=%d", tc.i, tc.n, tc.base, got, err, tc.want)
		}
	}
	if got := stats.Funcs[0].Peephole["if-affine-csinc"]; got != 1 {
		t.Fatalf("if-affine-csinc hits = %d, want 1", got)
	}
}
