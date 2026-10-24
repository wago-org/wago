//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	"os"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCTestFieldAdmission(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-test-field.wasm")
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
		t.Fatalf("root plan: %s", diagnostic)
	}
	var stats backend.ModuleStats
	c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true, GCFrameRoots: plan, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	want := map[string]int{"read": 1, "i64": 1, "root": 1, "packed-s": 1, "packed-u": 1, "nop": 0, "nullable": 0, "different-local": 0, "nonfinal": 0, "pressure": 1, "dirty": 1, "stack-root": 1, "budget": 16}
	for _, e := range m.Exports {
		n, ok := want[e.Name]
		if !ok {
			continue
		}
		s := stats.Funcs[e.Index.Index]
		if got := s.Peephole["gc-test-field-fuse"]; got != n {
			t.Errorf("%s fusions=%d want%d calls=%v", e.Name, got, n, s.Calls)
		}
		if e.Name == "read" && s.Calls["gcnative"] != 1 {
			t.Errorf("read resolver calls=%v want one", s.Calls)
		}
		if e.Name == "pressure" && s.PinnedLocals != 4 {
			t.Errorf("pressure pins=%d want4", s.PinnedLocals)
		}
		p := plan.Function(int(e.Index.Index))
		if p == nil {
			switch e.Name {
			case "read", "i64", "nop", "nullable", "different-local", "pressure", "dirty", "budget":
			default:
				t.Errorf("%s unexpectedly has no allocation root plan", e.Name)
			}
			delete(want, e.Name)
			continue
		}
		calls := 0
		if e.Name == "pressure" {
			calls = 1
		}
		if !p.Exact || p.CallMaskCount() != calls {
			t.Errorf("%s exact=%v call masks=%d mismatch", e.Name, p.Exact, p.CallMaskCount())
		}
		if e.Name == "root" {
			if p.SafepointCount() != 1 || s.Peephole["gc-dead-new"] != 0 || s.Peephole["gc-dead-new-checked"] != 0 {
				t.Fatalf("root allocation missing: safepoints=%d rewrites=%v", p.SafepointCount(), s.Peephole)
			}
			var saved uint32
			found := false
			for _, local := range p.Locals {
				if local.Index == 1 {
					saved, found = local.Offset, true
				}
			}
			if !found {
				t.Fatal("missing saved child root local1")
			}
			if !p.VisitSafepoints(func(_ int, offsets []uint32) bool {
				if len(offsets) != 1 || offsets[0] != saved {
					t.Errorf("allocation roots=%v want only child offset%d", offsets, saved)
				}
				return true
			}) {
				t.Fatal("invalid safepoint stream")
			}
		}
		t.Logf("%s fusions=%d code=%d frame=%d pins=%d calls=%v", e.Name, s.Peephole["gc-test-field-fuse"], s.CodeBytes, s.FrameBytes, s.PinnedLocals, s.Calls)
		delete(want, e.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing exports: %v", want)
	}
}
