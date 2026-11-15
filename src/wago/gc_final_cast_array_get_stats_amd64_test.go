//go:build linux && amd64 && !tinygo && wago_codegenstats

package wago

import (
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
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
	// Fresh allocations prove their adjacent casts before fusion is considered.
	// Keep those cases as checks for the stronger optimization.
	allocationCast := map[string]bool{}
	for _, name := range []string{"packed-s", "packed-u", "half-s", "half-u", "refs", "float", "vector", "roots", "float32", "int64"} {
		allocationCast[name] = true
		want[name] = 0
	}
	for _, e := range m.Exports {
		n, ok := want[e.Name]
		if !ok {
			continue
		}
		s := stats.Funcs[e.Index.Index]
		if got := s.Peephole["final-cast-array-get-fuse"]; got != n {
			t.Errorf("%s fused=%d want%d calls=%v", e.Name, got, n, s.Calls)
		}
		if allocationCast[e.Name] && s.Peephole["gc-allocation-cast-elide"] != 1 {
			t.Errorf("%s allocation cast removals=%d; want 1", e.Name, s.Peephole["gc-allocation-cast-elide"])
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

// An eqref parameter provides no array type proof. Every supported storage
// width must still select the checked cast/get fusion for this dynamic source.
func TestGCFinalCastDynamicArrayGetAdmission(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		storage, get, result byte
	}{
		{"i8-s", 0x78, 12, 0x7f}, {"i8-u", 0x78, 13, 0x7f},
		{"i16-s", 0x77, 12, 0x7f}, {"i16-u", 0x77, 13, 0x7f},
		{"i32", 0x7f, 11, 0x7f}, {"i64", 0x7e, 11, 0x7e},
		{"f32", 0x7d, 11, 0x7d}, {"f64", 0x7c, 11, 0x7c},
		{"v128", 0x7b, 11, 0x7b}, {"eqref", 0x6d, 11, 0x6d},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, tc.storage, 1}, []byte{0x60, 1, 0x6d, 1, tc.result})),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0x41, 0, 0xfb, tc.get, 0, 0x0b}))),
			)
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
			s := stats.Funcs[0]
			if s.Peephole["final-cast-array-get-fuse"] != 1 || s.Peephole["gc-allocation-cast-elide"] != 0 || s.Calls["hostsync"] != 0 {
				t.Fatalf("dynamic cast/get selection: rewrites=%v calls=%v", s.Peephole, s.Calls)
			}
		})
	}
}
