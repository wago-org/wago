//go:build amd64 && (wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2 || wago_gc_direct_weighted_sum || wago_gc_direct_weighted_sse2)

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
	key := "gc-compact-weighted-sum"
	simdKey := "gc-compact-weighted-sse2"
	if gcDirectWeighted {
		key = "gc-direct-weighted-sum"
		simdKey = "gc-direct-weighted-sse2"
	}
	if stats.Funcs[0].Peephole[key] != 1 {
		t.Fatalf("missing weighted sum %+v", stats.Funcs[0].Peephole)
	}
	if gcCompactWeightedSSE2 && stats.Funcs[0].Peephole[simdKey] != 1 {
		t.Fatal("missing weighted SSE2 selection")
	}
	for _, i := range []int{1, 2, 3} {
		if stats.Funcs[i].Peephole[key] != 0 {
			t.Fatalf("unexpected func%d selection", i)
		}
	}
}

// Reuse the same compiler state across admissions: a rejected second loop must
// erase the first loop's certificate rather than accidentally specialize it.
func TestCompactWeightedMatcherResets(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompArray, Array: wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Var)}}}}}}
	body := []byte{0x20, 2, 0x20, 1, 0x41, 3, 0x6a, 0x20, 0, 0x20, 1, 0xfb, 11, 0, 0x6c, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x41, 7, 0x49, 0x0d, 0, 0x0b}
	plan := gcLoopResolverPlan{local: 0, typeIndex: 0, endPC: uint32(len(body) - 1), accesses: 1}
	for _, tc := range []struct {
		name     string
		at       int
		value    byte
		paired   bool
		accesses uint16
	}{
		{"different-read-index", 10, 2, false, 1}, {"wrong-receiver", 8, 1, false, 1},
		{"different-sum-write", 17, 1, false, 1}, {"step-two", 21, 2, false, 1},
		{"different-index-write", 24, 2, false, 1}, {"zero-bound", 26, 0, false, 1},
		{"signed-bound", 27, 0x48, false, 1}, {"outer-branch", 29, 1, false, 1},
		{"sum-receiver-alias", 1, 0, false, 1}, {"sum-index-alias", 1, 1, false, 1},
		{"paired", -1, 0, true, 1}, {"two-accesses", -1, 0, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{m: m, localType: []machineType{mtI64, mtI32, mtI32}}
			r := wasm.ReaderFrom(body)
			f.prepareWeighted(&r, plan, false, directGCScalar{size: 4, typ: mtI32}, true)
			if f.weightedBound != 7 {
				t.Fatal("canonical pattern not selected")
			}
			b := append([]byte(nil), body...)
			if tc.at >= 0 {
				b[tc.at] = tc.value
			}
			r = wasm.ReaderFrom(b)
			p := plan
			p.accesses = tc.accesses
			f.prepareWeighted(&r, p, tc.paired, directGCScalar{size: 4, typ: mtI32}, true)
			if f.weightedBound != 0 {
				t.Fatal("rejected loop retained stale certificate")
			}
			if r.Offset() != 0 {
				t.Fatal("matcher advanced caller reader")
			}
			r = wasm.ReaderFrom(body)
			f.prepareWeighted(&r, plan, false, directGCScalar{size: 4, typ: mtI32}, true)
			if f.weightedBound != 7 {
				t.Fatal("subsequent valid loop not admitted")
			}
		})
	}
	for n := 0; n < len(body); n++ {
		f := fn{m: m, localType: []machineType{mtI64, mtI32, mtI32}}
		f.weightedBound = 99
		r := wasm.ReaderFrom(body[:n])
		f.prepareWeighted(&r, plan, false, directGCScalar{size: 4, typ: mtI32}, true)
		if f.weightedBound != 0 {
			t.Fatalf("truncated pattern length%d retained certificate", n)
		}
	}
}
