//go:build amd64 && wago_codegenstats

package main

import (
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestHeaderNopsAgainstCompiler(t *testing.T) {
	data, err := os.ReadFile("../../../corpus/workloads/synthetic/memory.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, where := range []int{0, 1, 2, 3} {
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		f := &m.Code[1]
		ts, ls, err := decode(m, f.BodyBytes)
		if err != nil {
			t.Fatal(err)
		}
		// Insert a nop before each header instruction, or just after br_if.
		pc := ts[ls[0].start+1+where].PC
		b := append([]byte(nil), f.BodyBytes[:pc]...)
		b = append(b, 1)
		b = append(b, f.BodyBytes[pc:]...)
		f.BodyBytes = b
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		ts, ls, err = decode(m, b)
		if err != nil {
			t.Fatal(err)
		}
		var ft wasm.CompType
		m.ResolveLocalFuncType(1, &ft)
		want := where < 3
		if exact(ts, ls[0], ft.Params, f.Locals.Runs) != want || !bodyMatch(ts, ls[0], ft.Params, f.Locals.Runs) {
			t.Fatalf("scanner nop position %d", where)
		}
		var stats amd64.ModuleStats
		cm, err := amd64.CompileModuleWith(m, amd64.CompileOptions{Workers: 1, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		if (stats.Funcs[1].Peephole["linear-sum-unroll4"] == 1) != want {
			t.Fatalf("compiler nop position %d: %v", where, stats.Funcs[1].Peephole)
		}
		cm.CodeImage.Close()
	}
}
