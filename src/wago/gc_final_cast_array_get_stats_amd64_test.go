//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"testing"
)

func TestGCFinalCastArrayGetAdmission(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-final-cast-array-get.wasm")
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
	var stats backend.ModuleStats
	c, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	want := map[string]int{"float32": 1, "int64": 1, "nonfinal": 0, "ref-loop": 1, "index-trap": 0, "roots": 1, "read": 1, "nullable": 1, "packed-s": 1, "packed-u": 1, "half-s": 1, "half-u": 1, "refs": 1, "float": 1, "vector": 1, "loop": 1, "pressure": 1, "pin-entry": 1, "negative-nop": 0, "negative-index": 0}
	for _, e := range m.Exports {
		n, ok := want[e.Name]
		if !ok {
			continue
		}
		s := stats.Funcs[e.Index.Index]
		if got := s.Peephole["final-cast-array-get-fuse"]; got != n {
			t.Errorf("%s fused=%d want%d calls=%v", e.Name, got, n, s.Calls)
		}
		if e.Name == "pin-entry" && s.PinnedGlobalsValue < 3 {
			t.Errorf("pin-entry value pins=%d want at least3", s.PinnedGlobalsValue)
		}
		t.Logf("%s fused=%d pins=%d code=%d calls=%v", e.Name, s.Peephole["final-cast-array-get-fuse"], s.PinnedGlobalsValue, s.CodeBytes, s.Calls)
		delete(want, e.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing exports:%v", want)
	}
}
