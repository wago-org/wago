//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCDynamicCopyPressureAdmission(t *testing.T) {
	for _, sites := range []int{1, 16, 17, 256} {
		testGCDynamicCopyPressureAdmission(t, sites)
	}
}

func testGCDynamicCopyPressureAdmission(t *testing.T, sites int) {
	t.Helper()
	m, err := wasm.DecodeModule(gcDynamicCopyPressureModuleSites(sites))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats backend.ModuleStats
	c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	if got, want := stats.Funcs[1].Peephole["gc-array-native-copy-dynamic"], min(sites, 16); got != want {
		t.Fatalf("sites%d dynamic copies=%d want%d", sites, got, want)
	}
}
