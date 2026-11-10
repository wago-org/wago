//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCRetainedResolverPhaseAdmission(t *testing.T) {
	for _, kind := range []string{"phase", "phase1", "phase2", "reverse1", "reverse8"} {
		t.Run(kind, func(t *testing.T) {
			m, err := wasm.DecodeModule(gcRetainedResolverModule(2, 16, kind))
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
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
			defer c.CodeImage.Close()
			// Each phase resolves its local once; the other fourteen reads reuse it.
			if got := stats.Funcs[1].GCHandleResolutionReuse; got != 14 {
				t.Fatalf("%s reuse%d want14", kind, got)
			}
		})
	}
}
