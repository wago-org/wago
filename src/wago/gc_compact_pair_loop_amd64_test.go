//go:build linux && amd64 && wago_gc_compact_pair_loop

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestCompactNativePairLoop(t *testing.T) {
	// run(count, sourceLength, targetLength), with fresh moving allocations.
	body := []byte{2, 2, 0x63, 0, 1, 0x7f,
		0x41, 7, 0x20, 1, 0xfb, 6, 0, 0x21, 3,
		0x41, 0, 0x20, 2, 0xfb, 6, 0, 0x21, 4,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40,
		0x20, 4, 0x20, 5, 0x20, 3, 0x20, 5, 0xfb, 11, 0, 0xfb, 14, 0,
		0x20, 5, 0x41, 1, 0x6a, 0x22, 5, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x0b,
		0x20, 5, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	in := compactLoopInstance(t, data)
	for _, n := range []uint64{0, 1, 2, 31, 127} {
		if got, e := in.Invoke("run", n, n, n); e != nil || got[0] != n {
			t.Fatalf("n%d: %v %v", n, got, e)
		}
	}
	for _, args := range [][3]uint64{{2, 1, 2}, {2, 2, 1}, {1, 0, 0}} {
		if _, e := in.Invoke("run", args[:]...); e == nil {
			t.Fatalf("missing bound trap %v", args)
		}
	}
}
