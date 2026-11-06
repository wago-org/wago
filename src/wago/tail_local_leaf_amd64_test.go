//go:build linux && amd64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLocalTailCycleRemainsUnboundedAmd64(t *testing.T) {
	body := func(result, target byte) []byte {
		return []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, result, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, target, 0x0b, 0x0b}
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body(5, 1)), wasmtest.Code(body(9, 0)))))
	for _, workers := range []int{1, 4} {
		for _, enabled := range []bool{false, true} {
			c, err := NewRuntimeConfig().WithFunctionWorkers(workers).WithOptimization("tail-self-loop", enabled).Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			if c.directPreparedBoundedAt(0) || c.directPreparedBoundedAt(1) || c.boundedNativeScalarLeaf() {
				c.Close()
				t.Fatal("mutual tail cycle admitted as bounded")
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
			for _, n := range []uint64{0, 1, 2, 31, 100000} {
				want := uint64(5)
				if n&1 != 0 {
					want = 9
				}
				got, err := fn.Invoke(n | 0xdeadbeef00000000)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("workers=%d enabled=%t n=%d got=%v,%v want=%d", workers, enabled, n, got, err, want)
				}
			}
			in.Close()
			c.Close()
		}
	}
}
