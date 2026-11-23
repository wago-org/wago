//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCGeneralScalarArraySetEligibility(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, storage := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x6d, 0x78, 0x77} {
		for _, open := range []bool{false, true} {
			array := []byte{0x5e, storage, 1}
			if open {
				array = append([]byte{0x50, 0}, array...)
			}
			value := storage
			if storage == 0x78 || storage == 0x77 {
				value = 0x7f
			}
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(array, []byte{0x60, 3, 0x63, 0, 0x7f, value, 0})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 14, 0, 0x0b}))))
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
			_, err = CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats, DeferCodeMapping: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if storage == 0x7f || storage == 0x7e || storage == 0x7d || storage == 0x7c {
				want = 1
			}
			if got := stats.Funcs[0].Peephole["gc-array-general-scalar-set-native"]; got != want {
				t.Errorf("storage%x open%v: native%d,want%d", storage, open, got, want)
			}
		}
	}
}
