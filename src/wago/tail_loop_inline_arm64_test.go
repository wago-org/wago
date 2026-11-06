//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestTailHelperCallerRemainsUnbounded(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x12, 1, 0x0b, 0x0b}))))
	for _, workers := range []int{1, 4} {
		c, err := NewRuntimeConfig().WithFunctionWorkers(workers).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		if c.directPreparedBoundedAt(0) || c.boundedNativeScalarLeaf() {
			c.Close()
			t.Fatalf("workers=%d admitted unbounded tail helper caller", workers)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 2, 31, 4096, 100000} {
			got, err := fn.Invoke(n|0xdeadbeef00000000, 0xfffffff0)
			want := uint64(uint32(0xfffffff0 + n*17))
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("workers=%d n=%d got=%v,%v want %d", workers, n, got, err, want)
			}
		}
		in.Close()
		c.Close()
	}
}
