//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCVectorArrayAccessStaysNative(t *testing.T) {
	requireCompilerDiagnostics(t)
	// (param (ref null $array) i32 v128) (result v128)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7b, 1}, []byte{0x60, 3, 0x63, 0, 0x7f, 0x7b, 1, 0x7b})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("access", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 14, 0,
			0x20, 0, 0x20, 1, 0xfb, 11, 0, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{GCArrayHelpers: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[0].GCCodeBytes.HelperCall; got != 0 {
		t.Fatalf("v128 array.get/set helper bytes = %d, want 0", got)
	}
}
