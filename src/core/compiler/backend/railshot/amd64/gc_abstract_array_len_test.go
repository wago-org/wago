//go:build amd64 && (!tinygo || !wago_lean)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCAbstractArrayCastLenFuses(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := gcSharedStubsEnabled
	defer func() { gcSharedStubsEnabled = saved }()
	for _, enabled := range []bool{true, false} {
		gcSharedStubsEnabled = enabled
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
			want := 0
			if enabled {
				want = 1
			}
			if n := stats.Funcs[0].Peephole["gc-abstract-array-cast-len-fuse"]; n != want {
				t.Fatalf("shared stubs=%v cast %d fused paths=%d, want %d; calls=%v", enabled, cast, n, want, stats.Funcs[0].Calls)
			}
			if !enabled && (stats.GCSharedStubs != 0 || stats.GCSharedStubCallSites != 0) {
				t.Fatal("disabled shared stubs still emitted an abstract array length leaf")
			}
		}
	}
}
