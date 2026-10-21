//go:build amd64 && (!tinygo || !wago_lean)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCAbstractArrayCastLenFuses(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, cast := range []byte{22, 23} {
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 1, 0x6e, 1, 0x7f})),
			wasmtest.Section(3, wasmtest.Vec([]byte{0})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("length", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, cast, 0x6a, 0xfb, 15, 0x0b}))),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err = wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		defer cm.CodeImage.Close()
		if n := stats.Funcs[0].Peephole["gc-abstract-array-cast-len-fuse"]; n != 1 {
			t.Fatalf("cast %d fused paths=%d, want 1; calls=%v", cast, n, stats.Funcs[0].Calls)
		}
	}
}
