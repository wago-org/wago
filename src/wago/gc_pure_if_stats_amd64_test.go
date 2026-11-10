//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCPureIfAdmission(t *testing.T) {
	for _, kind := range []string{"pure", "noelse", "assign", "call", "nested", "branch", "long"} {
		for _, regions := range []int{1, 4, 20} {
			m, err := wasm.DecodeModule(gcPureIfModule(4, regions, kind))
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats backend.ModuleStats
			c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true})
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			want := 0
			if kind == "pure" || kind == "noelse" {
				want = min(regions, 16)
			}
			if got := stats.Funcs[1].Peephole["gc-resolve-pure-if"]; got != want {
				t.Fatalf("%s/%d admission%d want%d", kind, regions, got, want)
			}
			reuse := regions*4 - 1 + want
			if kind == "call" {
				reuse = regions * 3
			}
			if got := stats.Funcs[1].GCHandleResolutionReuse; got != reuse {
				t.Fatalf("%s/%d reuse%d want%d", kind, regions, got, reuse)
			}
		}
	}
}

func TestGCPureIfPressureAdmission(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64} {
		m, err := wasm.DecodeModule(gcPureIfPressureModule(typ))
		if err != nil {
			t.Fatal(err)
		}
		if err = wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		var stats backend.ModuleStats
		c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		if got := stats.Funcs[1].Peephole["gc-resolve-pure-if"]; got != 1 {
			t.Fatalf("%v pure if admitted%d want1", typ, got)
		}
		if got := stats.Funcs[1].GCHandleResolutionReuse; got != 24 {
			t.Fatalf("%v reuse%d want24", typ, got)
		}
	}
}
