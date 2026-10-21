//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	"reflect"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDirectForwarderRootMasks(t *testing.T) {
	for _, numericParam := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			testDirectForwarderRootMasks(t, enabled, numericParam)
		}
	}
}

func testDirectForwarderRootMasks(t *testing.T, enabled, numericParam bool) {
	t.Helper()
	m, err := wasm.DecodeModule(directForwarderRootsModuleFor(numericParam))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var diagnostic string
	plan := newGCFrameRootPlan(m, true, &diagnostic, nil)
	if plan == nil || diagnostic != "" {
		t.Fatalf("root plan: %q", diagnostic)
	}
	var stats backend.ModuleStats
	cm, err := backend.CompileModuleWith(m, backend.CompileOptions{GCStructHelpers: true, GCFrameRoots: plan, Stats: &stats, Optimizations: map[string]bool{"direct-forwarder": enabled}})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	wantSites := 0
	if enabled {
		wantSites = 1
	}
	if got := stats.Funcs[5].Peephole["direct-forwarder-retarget"]; got != wantSites {
		t.Fatalf("enabled=%t: retargeted caller sites=%d, want%d", enabled, got, wantSites)
	}
	if numericParam && stats.Funcs[5].Calls[shared.CallMixed] != 1 {
		t.Fatalf("numeric-parameter/reference-result call must use its actual register ABI: %v", stats.Funcs[5].Calls)
	}
	p := plan.Function(5)
	if !p.Exact || p.CallsiteCount() != 2 || p.CallMaskCount() != 2 {
		t.Fatalf("source/native call accounting changed: %+v", p)
	}
	var got [][]uint32
	p.VisitCallsites(func(_ int, site shared.GCFrameCallsite) bool {
		got = append(got, append([]uint32(nil), site.Offsets()...))
		return true
	})
	want := [][]uint32{{p.Locals[0].Offset, p.Locals[1].Offset}, {p.Locals[0].Offset, p.Locals[1].Offset, p.Locals[2].Offset}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("call roots=%v, want%v", got, want)
	}
}

func TestDirectForwarderDirectEdgesOnly(t *testing.T) {
	m, err := wasm.DecodeModule(directForwarderTrapsModule())
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var diagnostic string
	plan := newGCFrameRootPlan(m, true, &diagnostic, nil)
	if plan == nil || diagnostic != "" {
		t.Fatalf("root plan: %q", diagnostic)
	}
	var stats backend.ModuleStats
	cm, err := backend.CompileModuleWith(m, backend.CompileOptions{GCStructHelpers: true, GCFrameRoots: plan, Stats: &stats, Optimizations: map[string]bool{"inline": false, "immutable-table": false}})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	for _, caller := range []int{3, 4, 5, 8, 9} {
		want := 1
		wantCalls := 1
		if caller == 4 || caller == 5 {
			want = 0
			// Existing indirect lowering records local, cross-instance and host
			// branches for its one source call. Retargeting changes none of them.
			wantCalls = 3
		}
		if got := stats.Funcs[caller].Peephole["direct-forwarder-retarget"]; got != want {
			t.Fatalf("function%d retarget=%d,want%d", caller, got, want)
		}
		if p := plan.Function(caller); !p.Exact || p.CallMaskCount() != 1 || p.CallsiteCount() != wantCalls {
			t.Fatalf("function%d source/native call accounting changed: %+v", caller, p)
		}
	}
	if stats.Funcs[9].PinnedGlobalsValue == 0 {
		t.Fatal("dirty-global trap fixture must use value pins")
	}
}
