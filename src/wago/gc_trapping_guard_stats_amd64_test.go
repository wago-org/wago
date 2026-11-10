//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"testing"
)

func TestGCTrappingGuardAdmission(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-trapping-guard.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, inline := range []bool{true, false} {
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
		c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true, GCFrameRoots: plan, Optimizations: map[string]bool{"inline": inline}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		want := map[string]int{"read": 1, "nullable": 1, "roots": 1, "dirty": 1, "noninline": 0, "store": 0, "local-write": 0, "both-arms": 0, "larger": 0, "different-type": 0, "budget": 0, "pressure": 0, "subtype": 0, "same-field": 1, "packed-s": 1, "packed-u": 1, "indexed-block": 1, "block-param": 0, "block-multi": 0}
		for _, e := range m.Exports {
			n, ok := want[e.Name]
			if !ok {
				continue
			}
			if !inline {
				n = 0
			}
			s := stats.Funcs[e.Index.Index]
			if e.Name == "pressure" && s.PinnedLocals != 7 {
				t.Errorf("pressure pins=%d want7; no-free-pin proof changed", s.PinnedLocals)
			}
			if got := s.Peephole["gc-trapping-guard-reuse"]; got != n {
				t.Errorf("inline=%v %s reuse=%d want%d calls=%v", inline, e.Name, got, n, s.Calls)
			}
			p := plan.Function(int(e.Index.Index))
			if e.Name == "roots" {
				if s.Peephole["gc-dead-new"] != 0 || s.Peephole["gc-dead-new-checked"] != 0 {
					t.Errorf("root retention test allocation was removed: %v", s.Peephole)
				}
				calls := 1
				if inline {
					calls = 0
				}
				if p.CallsiteCount() != calls || p.SafepointCount() != 1 {
					t.Errorf("roots source calls=%d want%d allocation safepoints=%d want1", p.CallsiteCount(), calls, p.SafepointCount())
				}
				var savedOffset uint32
				found := false
				for _, local := range p.Locals {
					if local.Index == 2 {
						savedOffset, found = local.Offset, true
					}
				}
				if !found {
					t.Fatal("missing retained root local2")
				}
				if !p.VisitSafepoints(func(_ int, offsets []uint32) bool {
					if len(offsets) != 1 || offsets[0] != savedOffset {
						t.Errorf("allocation roots=%v want only saved local offset%d", offsets, savedOffset)
					}
					return true
				}) {
					t.Fatal("invalid allocation root stream")
				}
			}

			if !p.Exact || p.CallMaskCount() != 1 {
				t.Errorf("%s exact=%v source masks=%d want1", e.Name, p.Exact, p.CallMaskCount())
			}
			if e.Name == "read" || e.Name == "nullable" || e.Name == "dirty" {
				calls := 1
				if inline {
					calls = 0
				}
				if p.CallsiteCount() != calls {
					t.Errorf("%s native calls=%d want%d", e.Name, p.CallsiteCount(), calls)
				}
			}
			t.Logf("inline=%v %s reuse=%d locals=%d globals=%d frame=%d spills=%d code=%d calls=%v", inline, e.Name, s.Peephole["gc-trapping-guard-reuse"], s.PinnedLocals, s.PinnedGlobalsValue, s.FrameBytes, s.Spills, s.CodeBytes, s.Calls)
			t.Logf("%s rewrites=%v", e.Name, s.Peephole)
			delete(want, e.Name)
		}
		if len(want) != 0 {
			t.Fatalf("missing exports:%v", want)
		}
	}
}

func TestGCTrappingGuardImportAdmission(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-trapping-guard-import.wasm")
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("roots: %s", diagnostic)
	}
	var stats backend.ModuleStats
	c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCFrameRoots: plan, Optimizations: map[string]bool{"inline": true}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
	if err != nil {
		t.Fatal(err)
	}
	c.CodeImage.Close()
	s := stats.Funcs[0]
	p := plan.Function(0)
	if s.Peephole["gc-trapping-guard-reuse"] != 0 || p.CallMaskCount() != 1 || p.CallsiteCount() != 1 || !p.Exact {
		t.Fatalf("import incorrectly reused: peeps=%v roots=%+v", s.Peephole, p)
	}
}
