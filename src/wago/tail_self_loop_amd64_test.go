//go:build linux && amd64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func selfTailLoopFixtureAmd64(indirect bool) []byte {
	body := []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a}
	if indirect {
		body = append(body, 0x41, 2, 0x13, 0, 0)
	} else {
		body = append(body, 0x12, 0)
	}
	body = append(body, 0x0b, 0x0b)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
	}
	if indirect {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 4})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))))
	if indirect {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 2, 0x0b, 1, 0})))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
	return wasmtest.Module(sections...)
}

func TestSelfTailLoopRemainsUnboundedAmd64(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		source := selfTailLoopFixtureAmd64(indirect)
		for _, workers := range []int{1, 4} {
			for _, enabled := range []bool{false, true} {
				c, err := NewRuntimeConfig().WithFunctionWorkers(workers).WithOptimization("tail-self-loop", enabled).Compile(source)
				if err != nil {
					t.Fatal(err)
				}
				if c.directPreparedBoundedAt(0) || c.boundedNativeScalarLeaf() {
					c.Close()
					t.Fatal("unbounded tail loop was admitted as bounded")
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
						t.Fatalf("workers=%d enabled=%t n=%d got=%v,%v want %d", workers, enabled, n, got, err, want)
					}
				}
				in.Close()
				c.Close()
			}
		}
	}
}
