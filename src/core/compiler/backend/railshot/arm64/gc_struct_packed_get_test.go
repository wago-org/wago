//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCPackedStructGetEligibility(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, test := range []struct {
		storage, result, op byte
		native              bool
	}{
		{0x78, 0x7f, 3, true}, {0x78, 0x7f, 4, true}, {0x77, 0x7f, 3, true}, {0x77, 0x7f, 4, true},
		{0x7f, 0x7f, 2, false}, {0x7e, 0x7e, 2, false}, {0x7b, 0x7b, 2, false}, {0x6d, 0x6d, 2, false},
	} {
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, test.storage, 1}, []byte{0x60, 1, 0x63, 0, 1, test.result})),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, test.op, 0, 0, 0x0b}))),
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
		if got := stats.Funcs[0].Peephole["gc-struct-packed-get-native"]; got != want {
			t.Errorf("storage%x: native packed getter sites=%d,want%d", test.storage, got, want)
		}
	}
}
