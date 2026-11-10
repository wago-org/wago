//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestGCNativeI31NoneAdmission(t *testing.T) {
	for _, op := range []byte{20, 21, 22, 23} {
		for _, heap := range []byte{0x6c, 0x71} {
			m, err := wasm.DecodeModule(gcNativeI31NoneLoop(op, heap, false))
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
			want := 1
			if op == 22 && heap == 0x71 {
				want = 0
			} // a non-nullable none cast cannot succeed
			if stats.Funcs[0].Peephole["gc-null-i31-ref"] != want {
				t.Fatalf("op%d heap%x: native paths %d, want %d", op, heap, stats.Funcs[0].Peephole["gc-null-i31-ref"], want)
			}
		}
	}
}
