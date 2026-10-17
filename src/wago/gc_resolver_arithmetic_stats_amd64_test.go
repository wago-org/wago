//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestGCResolverArithmeticReuseAdmission(t *testing.T) {
	for _, sites := range []int{1, 4, 16} {
		m, err := wasm.DecodeModule(gcResolverArithmeticModule(sites, false))
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
		got := stats.Funcs[1].GCHandleResolutionReuse
		c.CodeImage.Close()
		if got != sites-1 {
			t.Fatalf("sites%d reuse%d want%d", sites, got, sites-1)
		}
	}
}
