//go:build (linux && arm64) || (darwin && arm64)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestARM64I32ClzCtzWidth verifies that scalar count instructions use their
// 32-bit encodings. Using their 64-bit forms adds 32 to i32.clz and makes
// i32.ctz observe the wrong half of the register, corrupting allocator bitmaps.
func TestARM64I32ClzCtzWidth(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("clz", 0, 0),
			wasmtest.ExportEntry("ctz", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x67, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x68, 0x0b}),
		)),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), mod)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, tc := range []struct {
		arg, clz, ctz int32
	}{
		{0, 32, 32},
		{0x400, 21, 10},
		{-2147483648, 0, 31},
	} {
		for _, name := range []string{"clz", "ctz"} {
			r, err := in.Invoke(name, I32(tc.arg))
			if err != nil {
				t.Fatalf("%s(%#x): %v", name, uint32(tc.arg), err)
			}
			want := tc.clz
			if name == "ctz" {
				want = tc.ctz
			}
			if got := AsI32(r[0]); got != want {
				t.Fatalf("%s(%#x) = %d, want %d", name, uint32(tc.arg), got, want)
			}
		}
	}
}
