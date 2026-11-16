//go:build linux && amd64 && wago_gc_compact_weighted_sum

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func compactWeightedModule(weight, bound int32) []byte {
	body := []byte{1, 1, 0x63, 0, 0x20, 3, 0x20, 0, 0xfb, 6, 0, 0x21, 4, 0x03, 0x40,
		0x20, 2, 0x20, 1, 0x41}
	body = append(body, wasmtest.SLEB32(weight)...)
	body = append(body, 0x6a, 0x20, 4, 0x20, 1, 0xfb, 11, 0, 0x6c, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x41)
	body = append(body, wasmtest.SLEB32(bound)...)
	body = append(body, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}
func TestCompactNativeWeightedSum(t *testing.T) {
	for _, weight := range []int32{1, -7, 0x7fffffff, -0x80000000} {
		for _, bound := range []int32{1, 7, 32} {
			in := compactLoopInstance(t, compactWeightedModule(weight, bound))
			for _, first := range []uint32{0, uint32(bound) - 1, uint32(bound)} {
				for _, value := range []uint32{7, 0xffffffff, 0x7fffffff, 0x80000001} {
					index, sum := first, uint32(0xfffffff0)
					for {
						sum += (index + uint32(weight)) * value
						index++
						if index >= uint32(bound) {
							break
						}
					}
					got, e := in.Invoke("run", uint64(bound)+2, uint64(first), 0xfffffff0, uint64(value))
					if e != nil || len(got) != 2 || got[0] != uint64(index) || got[1] != uint64(sum) {
						t.Fatalf("weight%d bound%d first%d value%x: %v %v want%d,%x", weight, bound, first, value, got, e, index, sum)
					}
				}
			}
			for _, length := range []uint64{0, uint64(bound) - 1} {
				if _, e := in.Invoke("run", length, 0, 1, 7); e == nil {
					t.Fatalf("missing bounds weight%d bound%d length%d", weight, bound, length)
				}
			}
		}
	}
}
