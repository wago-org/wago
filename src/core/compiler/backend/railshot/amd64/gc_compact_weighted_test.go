//go:build amd64 && wago_gc_compact_weighted_sum

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"testing"
)

func TestCompactWeightedExactSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	data, e := os.ReadFile("../../../../../../bench/experiments/gc-v8/gc-array-loop-copy-checksum.wasm")
	if e != nil {
		t.Fatal(e)
	}
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	md, e := frontend.BuildGCTypeMetadata(m)
	if e != nil {
		t.Fatal(e)
	}
	stats := &ModuleStats{}
	cm, e := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: stats, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: md.Layouts}}})
	if e != nil {
		t.Fatal(e)
	}
	defer cm.CodeImage.Close()
	if stats.Funcs[0].Peephole["gc-compact-weighted-sum"] != 1 {
		t.Fatalf("missing weighted sum %+v", stats.Funcs[0].Peephole)
	}
	for _, i := range []int{1, 2, 3} {
		if stats.Funcs[i].Peephole["gc-compact-weighted-sum"] != 0 {
			t.Fatalf("unexpected func%d selection", i)
		}
	}
}
