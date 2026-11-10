//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestGCRetainedResolverAdmission(t *testing.T) {
	for _, objects := range []int{1, 2, 3} {
		m, err := wasm.DecodeModule(gcRetainedResolverModule(objects, 16, "plain"))
		if err != nil {
			t.Fatal(err)
		}
		if err = wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		var stats backend.ModuleStats
		c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		want := 15 / objects
		if got := stats.Funcs[1].GCHandleResolutionReuse; got != want {
			t.Fatalf("objects%d reuse%d want%d", objects, got, want)
		}
	}
}
