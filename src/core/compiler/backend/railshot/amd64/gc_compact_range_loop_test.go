//go:build amd64 && (wago_gc_compact_range_loop || (wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum) || wago_gc_compact_range_stride)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"testing"
)

func TestCompactRangeExactSelection(t *testing.T) {
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
	if stats.Funcs[2].Peephole["gc-compact-range-loop"] != 1 {
		t.Fatalf("no exact scalar range selection: %+v", stats.Funcs[2].Peephole)
	}
	if gcCompactRangeSSE2 && stats.Funcs[2].Peephole["gc-compact-range-sse2"] != 1 {
		t.Fatal("missing exact SSE2 selection")
	}
	if gcCompactRangeStride && stats.Funcs[2].Peephole["gc-compact-range-stride"] != 1 {
		t.Fatal("missing strength reduction")
	}
	if stats.Funcs[1].Peephole["gc-compact-range-loop"] != 0 {
		t.Fatal("bulk unexpectedly selected")
	}
}

func TestCompactRangeMatcher(t *testing.T) {
	body := []byte{0x20, 2, 0x20, 3, 0x20, 1, 0x20, 3, 0xfb, 11, 0, 0xfb, 14, 0, 0x20, 3, 0x41, 1, 0x6a, 0x22, 3, 0x41, 32, 0x49, 0x0d, 0, 0x0b}
	for _, tc := range []struct {
		name  string
		at    int
		value byte
		want  bool
	}{
		{"canonical", -1, 0, true}, {"offset-index", 7, 0, false}, {"step-two", 17, 2, false}, {"signed-bound", 23, 0x48, false}, {"zero-bound", 22, 0, false}, {"different-index", 20, 4, false}, {"wrong-destination", 1, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte(nil), body...)
			if tc.at >= 0 {
				b[tc.at] = tc.value
			}
			f := fn{localType: []machineType{mtI32, mtI64, mtI64, mtI32, mtI32}}
			r := wasm.ReaderFrom(b)
			f.prepareCompactRange(&r, gcLoopResolverPlan{local: 1, secondLocal: 2, typeIndex: 0, endPC: uint32(len(b) - 1), accesses: 2})
			if got := f.rangeBound != 0; got != tc.want {
				t.Fatalf("selected%v", got)
			}
			if r.Offset() != 0 {
				t.Fatal("reader advanced")
			}
		})
	}
}
