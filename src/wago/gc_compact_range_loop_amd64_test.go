//go:build linux && amd64 && (wago_gc_compact_range_loop || wago_gc_compact_range_stride)

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func compactRangeModule(storage, get, value, bound byte, alias bool) []byte {
	body := []byte{2, 2, 0x63, 0, 1, 0x7f, 0x41, value, 0x20, 0, 0xfb, 6, 0, 0x21, 2}
	if alias {
		body = append(body, 0x20, 2, 0x21, 3)
	} else {
		body = append(body, 0x41, 0, 0x20, 1, 0xfb, 6, 0, 0x21, 3)
	}
	body = append(body, 0x20, 3, 0x24, 0, 0x03, 0x40,
		0x20, 3, 0x20, 4, 0x20, 2, 0x20, 4, 0xfb, get, 0, 0xfb, 14, 0,
		0x20, 4, 0x41, 1, 0x6a, 0x22, 4, 0x41, bound, 0x49, 0x0d, 0, 0x0b,
		0x20, 4, 0x20, 3, 0x41, bound-1, 0xfb, get, 0, 0x0b)
	peek := []byte{0, 0x23, 0, 0x20, 0, 0xfb, get, 0, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, storage, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("peek", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), append(wasmtest.ULEB(uint32(len(peek))), peek...))))
}
func TestCompactNativeRangeLoop(t *testing.T) {
	for _, tc := range []struct {
		name                string
		storage, get, value byte
		want                uint64
	}{{"i32", 0x7f, 11, 7, 7}, {"i8", 0x78, 12, 0x79, 0xfffffff9}, {"i16", 0x77, 12, 0x79, 0xfffffff9}} {
		for _, bound := range []byte{1, 32} {
			for _, alias := range []bool{false, true} {
				t.Run(tc.name, func(t *testing.T) {
					in := compactLoopInstance(t, compactRangeModule(tc.storage, tc.get, tc.value, bound, alias))
					for _, n := range []uint64{uint64(bound), uint64(bound) + 1, 127} {
						got, e := in.Invoke("run", n, n)
						if e != nil || got[0] != uint64(bound) || got[1] != tc.want {
							t.Fatalf("bound%d alias%v n%d: %v %v", bound, alias, n, got, e)
						}
					}
					if alias {
						return
					}
					for _, a := range [][2]uint64{{0, 32}, {32, 0}, {uint64(bound) - 1, 32}, {32, uint64(bound) - 1}} {
						if _, e := in.Invoke("run", a[:]...); e == nil {
							t.Fatalf("missing trap %v", a)
						}
						prefix := min(a[0], a[1], uint64(bound))
						for i := uint64(0); i < a[1]; i++ {
							got, e := in.Invoke("peek", i)
							want := uint64(0)
							if i < prefix {
								want = tc.want
							}
							if e != nil || got[0] != want {
								t.Fatalf("partial write %v idx%d: %v %v want%d", a, i, got, e, want)
							}
						}
					}
				})
			}
		}
	}
}
