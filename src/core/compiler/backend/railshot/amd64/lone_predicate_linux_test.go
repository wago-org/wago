//go:build linux && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestLonePredicateExecution(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := lonePredicateEnabled
	defer func() { lonePredicateEnabled = saved }()
	for _, branch := range []bool{false, true} {
		body := []byte{0, 0x20, 0, 0x41, 1, 0x73, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 9, 0x0b, 0x0b}
		if branch {
			body = []byte{0, 0x02, 0x40, 0x20, 0, 0x41, 1, 0x73, 0x0d, 0, 0x41, 7, 0x0f, 0x0b, 0x41, 9, 0x0b}
		}
		m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
		var baselineBytes int
		for _, enabled := range []bool{false, true} {
			lonePredicateEnabled = enabled
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if enabled {
				if stats.Funcs[0].Peephole["lone-predicate"] == 0 {
					t.Fatal("fixture did not exercise admission")
				}
				if stats.Funcs[0].CodeBytes >= baselineBytes {
					t.Fatalf("code did not shrink: %d >= %d", stats.Funcs[0].CodeBytes, baselineBytes)
				}
			} else {
				baselineBytes = stats.Funcs[0].CodeBytes
			}
			for _, arg := range []int32{0, 1, 2, -1} {
				want := int32(7)
				if arg^1 == 0 {
					want = 9
				}
				if branch {
					want = 9
					if arg^1 == 0 {
						want = 7
					}
				}
				if got := runAmd64(t, m, arg); got != want {
					t.Fatalf("branch=%v enabled=%v arg=%v got=%v want=%v", branch, enabled, arg, got, want)
				}
			}
		}
	}
}
