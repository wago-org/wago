//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestTupleSelectionTrappingConditionRetainsPrefixTrapOrder(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, divS, divU, eqz, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x6d), byte(0x6e), byte(0x45), byte(0x6a)
		minimum := uint64(0x80000000)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, divS, divU, eqz, add = wasm.I64, 0x7e, 0x42, 0x7f, 0x80, 0x50, 0x7c
			minimum = 0x8000000000000000
			fold = func(v uint64) uint64 { return v }
		}
		// The older signed division must trap before the helper's unsigned
		// division. A trapping condition keeps the ordinary conditional path,
		// including inside a proved fallthrough-only tuple helper.
		caller := []byte{0, 0x20, 0, 0x20, 2, divS, 0x02, bt, 0x20, 0, 0x20, 1, 0x10, 1, 0x1a, 0x0b, add, 0x0b}
		helper := []byte{0, 0x20, 0, 0x20, 1, divU, eqz, 0x04, 2,
			0x20, 0, 0x20, 1, 0x05, 0x20, 1, 0x20, 0, 0x0b, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper},
			funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
		ft, _ := m.LocalFuncType(1)
		if !integerTupleSelectTarget(m, m.Code[1], ft) {
			t.Fatal("fixture does not exercise the fallthrough tuple class")
		}
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, tc := range []struct {
					a, b, c uint64
					trap    rt.TrapCode
				}{{17, 3, 1, 0}, {3, 17, 1, 0}, {17, 0, 1, rt.TrapDivZero}, {minimum, 0, ^uint64(0), rt.TrapDivOverflow}} {
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, tc.a, tc.b, tc.c)
					if tc.trap != 0 {
						var trap *rt.TrapError
						if !errors.As(err, &trap) || trap.Code != tc.trap {
							t.Fatalf("wide=%t inline=%t workers=%d input=%v err=%v want trap=%v", wide, enabled, workers, tc, err, tc.trap)
						}
					} else {
						selected := tc.a
						if tc.a/tc.b != 0 {
							selected = tc.b
						}
						want := fold(tc.a + selected) // c=1 in nontrapping cases
						if err != nil || got != want {
							t.Fatalf("wide=%t inline=%t workers=%d input=%v got=%x err=%v want=%x", wide, enabled, workers, tc, got, err, want)
						}
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["straight-line-block"] != 0) != (enabled && !profileEnabled) {
						t.Fatalf("inline=%t profile=%t did not exercise enclosing-block path: %v", enabled, profileEnabled, stats.Funcs[0].Peephole)
					}
					if diagnosticsEnabled && stats.Funcs[0].Peephole["tuple-if-select"] != 0 {
						t.Fatal("trapping condition did not retain its conditional fallback")
					}
				}
			}
		}
	}
}
