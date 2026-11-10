//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestDivRemConstPairAcrossPureOperands(t *testing.T) {
	saved, scalar := divRemConstPairEnabled, sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { divRemConstPairEnabled, sharedScalarEnabled = saved, scalar }()
	for _, gap := range []int{0, 1, 4, 8, 9, 16} {
		for _, d := range []uint32{3, 7, 10, 17, 400, 1000, 0xffffffff} {
			body := []byte{0, 0x20, 0, 0x41}
			body = append(body, wasmtest.SLEB32(int32(d))...)
			body = append(body, 0x6e)
			for range gap {
				body = append(body, 0x20, 1)
			}
			body = append(body, 0x20, 0, 0x41)
			body = append(body, wasmtest.SLEB32(int32(d))...)
			body = append(body, 0x70)
			body = append(body, 0x10, 1, 0x0f, 0x0b)
			// An ordinary call realizes arguments in source order before staging.
			// Keeping the gap as separate arguments also exercises the scanner limit
			// without triggering the unrelated deferred-expression depth limit.
			packetTypes := make([]wasm.ValType, gap+2)
			sum := []byte{0, 0x20, 0}
			for i := range packetTypes {
				packetTypes[i] = wasm.I32
				if i != 0 {
					sum = append(sum, 0x20, byte(i), 0x6a)
				}
			}
			sum = append(sum, 0x0b)
			m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body}, funcDef{packetTypes, []wasm.ValType{wasm.I32}, sum})

			for _, enabled := range []bool{false, true} {
				divRemConstPairEnabled = enabled
				var stats ModuleStats
				c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": false, "interval-region-pins": false, "interval-control": false}})
				if err != nil {
					t.Fatal(err)
				}
				for _, n := range []uint32{0, 1, d - 1, d, d + 1, 0x7fffffff, 0x80000000, 0xffffffff} {
					for _, prefix := range []uint32{0, 17, 0xffffffff} {
						want := n/d + n%d + uint32(gap)*prefix
						if got := runCompiledAmd64u(t, c, uint64(n)|0xdeadbeef00000000, uint64(prefix)|0xcafebabe00000000); got != uint64(want) {
							t.Fatalf("gap=%d d=%d n=%x prefix=%x enabled=%t got=%x want=%x", gap, d, n, prefix, enabled, got, want)
						}
					}
				}
				if c.CodeImage != nil {
					c.CodeImage.Close()
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["divrem-const-pair"] != 0) != (enabled && gap <= 8) {
					t.Fatalf("gap=%d d=%d enabled=%t pair selection mismatch", gap, d, enabled)
				}
			}
		}
	}
}
