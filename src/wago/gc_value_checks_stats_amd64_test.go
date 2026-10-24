//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	"os"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCValueChecksAdmission(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-value-checks.wasm")
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	meta, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic string
	plan := newGCFrameRootPlan(m, true, true, &diagnostic, nil)
	if plan == nil || diagnostic != "" {
		t.Fatalf("root plan: %s", diagnostic)
	}
	var stats backend.ModuleStats
	c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true, GCFrameRoots: plan, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()

	seen := map[string]bool{}
	total := 0
	for _, st := range stats.Funcs {
		if st == nil {
			continue
		}
		n := st.Peephole["gc-value-check-reuse"]
		if n != 0 || st.Peephole["gc-value-check-plan"] != 0 {
			t.Logf("%s all=%v reuse=%d save=%d extend=%d frame=%d", st.Name, st.Peephole, n, st.Peephole["gc-value-check-save"], st.Peephole["gc-value-check-extend"], st.FrameBytes)
		}
		if (st.Name == "after-call" || st.Name == "write") && st.Peephole["gc-value-check-plan"] != 0 {
			seen[st.Name] = true
			want := 1
			if st.Name == "write" {
				want = 2
			}
			if st.Peephole["gc-value-check-save"] == 0 || n != want {
				t.Fatalf("%s effect case not exercising one save/reuse before effect: %v", st.Name, st.Peephole)
			}
		}
		total += n
		if st.Name == "typed-children" && st.Peephole["gc-value-check-plan"] != 0 {
			seen[st.Name] = true
			if n != 1 || st.Peephole["gc-value-check-save"] != 1 {
				t.Fatalf("direct child read lost value identity: %v", st.Peephole)
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("missing effect fixture names: %v", seen)
	}
	if total < 8 {
		t.Fatalf("expected real native reuse, got%d", total)
	}
}
