//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTupleSelectionRepeatedStoreKeepsSourceOrder(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, le, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x4d), byte(0x6a)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, le, add = wasm.I64, 0x7e, 0x42, 0x58, 0x7c
			fold = func(x uint64) uint64 { return x }
		}
		// Both results target the same caller local. The second assignment wins,
		// while the prefix must retain the original input and local 1 stays intact.
		caller := []byte{0, 0x20, 0, 0x02, bt, 0x20, 0, 0x20, 1, 0x10, 1,
			0x21, 0, 0x21, 0, 0x20, 0, 0x20, 1, add, 0x0b, add, 0x0b}
		helper := []byte{0, 0x20, 0, 0x20, 1, le, 0x04, 2,
			0x20, 0, 0x20, 1, 0x05, 0x20, 1, 0x20, 0, 0x0b, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, caller},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper},
			funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, input := range [][2]uint64{{3, 7}, {17, 5}, {0, 0}, {0xdeadbeefffffffff, 0xcafebabe00000001}, {^uint64(0), 0x8000000000000001}} {
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, input[0], input[1])
					a, b := fold(input[0]), fold(input[1])
					want := fold(a + min(a, b) + b)
					if err != nil || got != want {
						t.Fatalf("wide=%t inline=%t workers=%d input=%x got=%x err=%v want=%x", wide, enabled, workers, input, got, err, want)
					}
					if diagnosticsEnabled && stats.Funcs[0].Peephole["tuple-if-select-local-stores"] != 0 {
						t.Fatal("repeated destination incorrectly consumed as parallel result stores")
					}
				}
			}
		}
	}
}
