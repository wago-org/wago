//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCScalarStructSetEligibility(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, test := range []struct {
		storage, result byte
		native          bool
	}{
		{0x7f, 0x7f, true}, {0x7e, 0x7e, true}, {0x7d, 0x7d, true}, {0x7c, 0x7c, true},
		{0x78, 0x7f, false}, {0x77, 0x7f, false}, {0x7b, 0x7b, false}, {0x6d, 0x6d, false},
	} {
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, test.storage, 1}, []byte{0x60, 2, 0x63, 0, test.result, 0})),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfb, 5, 0, 0, 0x0b}))),
		)
		m, err := wasm.DecodeModule(data)
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
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats, DeferCodeMapping: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}})
		if err != nil {
			t.Fatal(err)
		}
		_ = c
		want := 0
		if test.native {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["gc-struct-scalar-set-native"]; got != want {
			t.Errorf("storage%x: native setter sites=%d,want%d", test.storage, got, want)
		}
	}
}
