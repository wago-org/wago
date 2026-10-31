//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCGeneralScalarArrayGetEligibility(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, storage := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x6d} {
		for _, open := range []bool{false, true} {
			array := []byte{0x5e, storage, 1}
			if open {
				array = append([]byte{0x50, 0}, array...)
			}
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(array, []byte{0x60, 2, 0x63, 0, 0x7f, 1, storage})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfb, 11, 0, 0x0b}))))
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
			if storage != 0x7b && storage != 0x6d {
				want = 1
			}
			if got := stats.Funcs[0].Peephole["gc-array-general-scalar-get-native"]; got != want {
				t.Errorf("storage%x open%v: general native sites%d,want%d", storage, open, got, want)
			}
			wantNursery := 0
			if !open && want != 0 {
				wantNursery = 1
			}
			if got := stats.Funcs[0].Peephole["gc-array-scalar-native"]; got != wantNursery {
				t.Errorf("storage%x open%v: retained nursery sites%d,want%d", storage, open, got, wantNursery)
			}
		}
	}
}
