//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestGCDeadDefaultGroupAdmission(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5} {
		for _, fields := range []int{1, 4, 16, 17} {
			for _, kind := range []string{"plain", "local", "prefix", "gap", "mixed"} {
				m, err := wasm.DecodeModule(gcDeadGroupModule(n, fields, kind))
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
				c.CodeImage.Close()
				if kind == "plain" {
					t.Logf("group-frame n%d fields%d frame%d spill%d code%d", n, fields, stats.Funcs[0].FrameBytes, stats.Funcs[0].MaxSpillSlots, stats.Funcs[0].CodeBytes)
				}
				want := 0
				if n >= 2 && fields <= 16 && (kind == "plain" || kind == "mixed") {
					want = 1
				}
				if got := stats.Funcs[0].Peephole["gc-dead-default-group"]; got != want {
					t.Fatalf("n%d fields%d kind%s: groups%d want%d", n, fields, kind, got, want)
				}
			}
		}
	}
}
