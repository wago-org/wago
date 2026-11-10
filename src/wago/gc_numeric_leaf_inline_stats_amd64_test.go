//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	"reflect"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCNumericLeafInlineTrapPins(t *testing.T) {
	m, err := wasm.DecodeModule(gcNumericLeafTrapModule())
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var diagnostic string
	plan := newGCFrameRootPlan(m, true, true, &diagnostic, nil)
	if plan == nil || diagnostic != "" {
		t.Fatalf("root plan: %q", diagnostic)
	}
	var stats backend.ModuleStats
	cm, err := backend.CompileModuleWith(m, backend.CompileOptions{GCStructHelpers: true, GCFrameRoots: plan, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	for _, caller := range []int{2, 3} {
		s := stats.Funcs[caller]
		if s.PinnedGlobalsValue == 0 || s.Peephole["gc-numeric-leaf-inline"] != 1 {
			t.Fatalf("caller %d must inline division with live global pins: pins=%d inline=%d", caller, s.PinnedGlobalsValue, s.Peephole["gc-numeric-leaf-inline"])
		}
		if p := plan.Function(caller); !p.Exact || p.CallsiteCount() != 0 || p.CallMaskCount() != 1 {
			t.Fatalf("caller %d lost exact source-call accounting: %+v", caller, p)
		}
	}
}

func TestGCNumericLeafInlineAdmission(t *testing.T) {
	for _, inline := range []bool{false, true} {
		m, err := wasm.DecodeModule(gcNumericLeafInlineModule())
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		var diagnostic string
		plan := newGCFrameRootPlan(m, true, true, &diagnostic, nil)
		if plan == nil || diagnostic != "" || plan.Function(3).CallMaskCount() != 4 {
			t.Fatalf("invalid source root plan: %+v (%q)", plan, diagnostic)
		}
		var stats backend.ModuleStats
		cm, err := backend.CompileModuleWith(m, backend.CompileOptions{GCStructHelpers: true, GCFrameRoots: plan, Stats: &stats, Optimizations: map[string]bool{"inline": inline}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		wantInline, wantCalls := 0, 4
		if inline {
			wantInline, wantCalls = 2, 2
		}
		if got := stats.Funcs[3].Peephole["gc-numeric-leaf-inline"]; got != wantInline {
			t.Fatalf("inline=%t: selected numeric leaves = %d, want %d", inline, got, wantInline)
		}
		p := plan.Function(3)
		if !p.Exact || p.CallsiteCount() != wantCalls || p.CallMaskCount() != 4 {
			t.Fatalf("inline=%t: exact=%t calls=%d masks=%d; want exact, calls=%d masks=4", inline, p.Exact, p.CallsiteCount(), p.CallMaskCount(), wantCalls)
		}
		if inline {
			var got [][]uint32
			p.VisitCallsites(func(_ int, site shared.GCFrameCallsite) bool {
				got = append(got, append([]uint32(nil), site.Offsets()...))
				return true
			})
			want := [][]uint32{{p.Locals[0].Offset}, {p.Locals[0].Offset, p.Locals[1].Offset}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("allocating-call roots = %v, want %v", got, want)
			}
		}
	}
}
