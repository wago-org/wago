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

func TestGCReadRegionsAdmission(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-read-regions.wasm")
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

	want := map[string]int{"read": 1, "loop": 1, "roots": 5, "stack-roots": 3, "branch": 2, "forward": 1, "arms": 1, "late": 1, "nested": 2, "join": 1, "pressure": 1, "two-targets": 3, "different": 4, "pressure-root": 2, "many": 16, "home-branch": 1, "after-trap": 0, "pressure-loop": 1}
	for _, exp := range m.Exports {
		n, ok := want[exp.Name]
		if !ok {
			continue
		}
		st := stats.Funcs[exp.Index.Index]
		if got := st.Peephole["gc-read-region-inline"]; got != n {
			t.Errorf("%s regions=%d want%d calls=%v", exp.Name, got, n, st.Calls)
		}
		if (exp.Name == "pressure" || exp.Name == "pressure-root" || exp.Name == "home-branch" || exp.Name == "pressure-loop") && st.Peephole["gc-read-region-stack-cache"] == 0 {
			t.Fatalf("pressure did not exercise private address home: %v", st.Peephole)
		}
		t.Logf("%s regions=%d calls=%v frame=%d spills=%d", exp.Name, st.Peephole["gc-read-region-inline"], st.Calls, st.FrameBytes, st.MaxSpillSlots)
		delete(want, exp.Name)
	}
	if len(want) > 0 {
		t.Fatalf("missing exports=%v", want)
	}
}
