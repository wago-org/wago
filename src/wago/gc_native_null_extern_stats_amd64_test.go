//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestGCNativeNullExternAdmission(t *testing.T) {
	m, err := wasm.DecodeModule(gcNativeNullExternModule())
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithFeatures(m, wasm.ValidationFeatures{GCConstExpr: true}); err != nil {
		t.Fatal(err)
	}
	var stats backend.ModuleStats
	c, err := backend.CompileModuleWith(m, backend.CompileOptions{GCStructHelpers: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	c.CodeImage.Close()
	for index, want := range []int{0, 1, 1, 2, 2, 1, 1, 1, 1} {
		if got := stats.Funcs[index].Peephole["gc-null-extern-convert"]; got != want {
			t.Fatalf("function%d null paths%d,want%d", index, got, want)
		}
	}
}
