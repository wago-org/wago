//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"testing"
)

func TestDirectReferenceABITailChainSelection(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/direct-reference-tail-roots.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err = wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		meta, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		var diagnostic string
		plan := newGCFrameRootPlan(m, true, true, &diagnostic, nil)
		if plan == nil || diagnostic != "" {
			t.Fatalf("root plan: %s", diagnostic)
		}
		var stats backend.ModuleStats
		cm, err := backend.CompileModuleWith(m, backend.CompileOptions{ImportBindings: []backend.ImportBinding{}, GCStructHelpers: true, GCArrayHelpers: true, GCFrameRoots: plan, Stats: &stats, Optimizations: map[string]bool{"inline": false, "direct-reference-abi": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		// Each group is source-tail, ordinary reference caller, then hidden-root caller.
		for _, group := range [][3]int{{1, 2, 3}, {5, 6, 7}, {9, 10, 11}} {
			source, middle, caller := group[0], group[1], group[2]
			want := 0
			if enabled {
				want = 1
			}
			if got := stats.Funcs[caller].Calls[shared.CallMixed]; got != want {
				t.Errorf("enabled=%t caller%d register calls=%d want%d: %v", enabled, caller, got, want, stats.Funcs[caller].Calls)
			}
			if got := stats.Funcs[middle].Calls[shared.CallWrapper]; got != 1 {
				t.Errorf("own-tail source%d must keep wrapper call: %v", source, stats.Funcs[middle].Calls)
			}
			if cm.Entry[source] != cm.InternalEntry[source] {
				t.Errorf("source-tail%d acquired register entry", source)
			}
			p := plan.Function(caller)
			if p == nil || !p.Exact || p.CallMaskCount() != 1 || p.CallsiteCount() != 1 {
				t.Fatalf("caller%d root plan=%+v", caller, p)
			}
			site, ok := p.Callsite(0)
			if !ok || len(site.Offsets()) != 1 {
				t.Fatalf("caller%d must keep exactly its hidden reference", caller)
			}
		}
		if cm.Entry[0] != cm.InternalEntry[0] {
			t.Error("incoming direct-tail/addressable target acquired register entry")
		}
	}
}
