//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCConstructorIfAdmission(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		for _, kind := range []string{"terminal", "nested", "later", "limit17", "trap"} {
			m, err := wasm.DecodeModule(gcConstructorIfModule(typ, kind))
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats backend.ModuleStats
			c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true})
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			want := 0
			if kind == "terminal" || kind == "trap" {
				want = 1
			}
			if got := stats.Funcs[0].Peephole["gc-constructor-if-scalar-replace"]; got != want {
				t.Fatalf("%v/%s: admitted%d want%d", typ, kind, got, want)
			}
		}
	}
}

func TestGCConstructorIfPressureAdmission(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64} {
		m, err := wasm.DecodeModule(gcConstructorIfPressureModule(typ))
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		var stats backend.ModuleStats
		c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		if got := stats.Funcs[1].Peephole["gc-constructor-if-scalar-replace"]; got != 1 {
			t.Fatalf("%v: admitted%d want1", typ, got)
		}
	}
}
