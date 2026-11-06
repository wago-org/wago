//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

// Repeated splices must rebind private parameters without changing the older
// caller operand below the tuple or the results produced by the previous site.
func TestInlineBoundedTupleLoopRepeatedSites(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, add, sub, xor := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6a), byte(0x6b), byte(0x73)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, eqz, add, sub, xor = wasm.I64, 0x7e, 0x42, 0x50, 0x7c, 0x7d, 0x85
			fold = func(x uint64) uint64 { return x }
		}
		helper := []byte{1, 1, bt, 0x02, 0x40, 0x03, 0x40, 0x20, 0, eqz, 0x0d, 1,
			0x20, 1, 0x20, 2, add, 0x21, 3, 0x20, 2, 0x21, 1, 0x20, 3, 0x21, 2,
			0x20, 0, k, 1, sub, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x20, 2, 0x0b}
		caller := []byte{1, 2, bt, 0x20, 1, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, 0x21, 3,
			0x20, 0, 0x20, 4, 0x20, 3, 0x10, 1, 0x21, 4, 0x21, 3, 0x20, 3, 0x20, 4, xor, add, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, helper})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, n := range []uint64{0, 1, 2, 3, 7, 21} {
					for _, input := range [][2]uint64{{0, 1}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
						a, b := fold(input[0]), fold(input[1])
						original := a
						for range n {
							a, b = b, fold(a+b)
						}
						a, b = b, a
						for range n {
							a, b = b, fold(a+b)
						}
						want := fold(original + (a ^ b))
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, _, err := runMemAmd64WithOptions(t, m, opts, nil, n, input[0], input[1])
						if err != nil || got != want {
							t.Fatalf("wide=%t inline=%t workers=%d n=%d input=%x got=%x err=%v want=%x", wide, enabled, workers, n, input, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] == 2) != (enabled && !profileEnabled) {
							t.Fatalf("inline=%t profile=%t calls=%v", enabled, profileEnabled, stats.Funcs[0].Calls)
						}
					}
				}
			}
		}
	}
}
