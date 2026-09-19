//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func callFreeLoopExitModuleAMD64(t testing.TB) *wasm.Module {
	t.Helper()
	i32 := []wasm.ValType{wasm.I32}
	return modFuncs(t,
		funcDef{i32, i32, []byte{
			0x01, 0x01, 0x7f,
			0x20, 0x00, 0x21, 0x01,
			0x02, 0x40,
			0x03, 0x40,
			0x20, 0x01, 0x45, 0x0d, 0x01,
			0x20, 0x01, 0x41, 0x01, 0x6b, 0x21, 0x01,
			0x0c, 0x00,
			0x0b, 0x0b,
			0x20, 0x00, 0x10, 0x01,
			0x0b,
		}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func TestCallFreeLoopExitReconciliationIsColdAMD64(t *testing.T) {
	m := callFreeLoopExitModuleAMD64(t)
	compile := func(enabled bool) *ModuleStats {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Stats: &stats,
			Optimizations: map[string]bool{
				"inline":                  false,
				"callfree-loop-cold-exit": enabled,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		return &stats
	}
	on, off := compile(true), compile(false)
	if got := on.Funcs[0].Peephole["callfree-loop-exit-cold"]; got != 1 {
		t.Fatalf("cold loop exits = %d, want 1 (all: %v)", got, on.Funcs[0].Peephole)
	}
	if got := off.Funcs[0].Peephole["callfree-loop-exit-cold"]; got != 0 {
		t.Fatalf("disabled cold loop exits = %d, want 0", got)
	}
	for _, n := range []uint64{0, 1, 2, 17, 255} {
		cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"inline": false}})
		if err != nil {
			t.Fatal(err)
		}
		got := runCompiledAmd64u(t, cm, n)
		cm.CodeImage.Close()
		if got != n {
			t.Fatalf("f(%d) = %d, want %d", n, got, n)
		}
	}
}
