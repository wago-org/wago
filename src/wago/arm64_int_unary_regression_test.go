//go:build (linux && arm64) || (darwin && arm64)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
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

func TestARM64DraglineFibonacciGroupedRemainders(t *testing.T) {
	locals := append(wasmtest.ULEB(3), byte(0x7e))
	body := append(wasmtest.Vec(locals), []byte{
		0x42, 0x00, 0x21, 0x01, // a = 0
		0x42, 0x01, 0x21, 0x02, // b = 1
		0x02, 0x40, 0x03, 0x40, // block; loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // break when n == 0
		0x20, 0x01, 0x20, 0x02, 0x7c, 0x21, 0x03, // t = a + b
		0x20, 0x02, 0x21, 0x01, // a = b
		0x20, 0x03, 0x21, 0x02, // b = t
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n--
		0x0c, 0x00, 0x0b, 0x0b, // continue; end loop/block
		0x20, 0x01, 0x0b,
	}...)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fib", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for n := int32(0); n <= 96; n++ {
		a, b := uint64(0), uint64(1)
		for range n {
			a, b = b, a+b
		}
		result, err := instance.Invoke("fib", I32(n))
		if err != nil || len(result) != 1 || uint64(AsI64(result[0])) != a {
			t.Fatalf("fib(%d) = %v, %v; want %d", n, result, err, a)
		}
	}
}
