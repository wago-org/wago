//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// One adjacent cast/load needs one checked object resolution. Two resolutions
// duplicate the handle walk; a Go call is unnecessary for a vector field too.
func TestGCFinalCastFieldReadResolvesOnce(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name                string
		storage, result, op byte
	}{
		{"i32", 0x7f, 0x7f, 2}, {"i64", 0x7e, 0x7e, 2},
		{"f32", 0x7d, 0x7d, 2}, {"f64", 0x7c, 0x7c, 2},
		{"i8-s", 0x78, 0x7f, 3}, {"i8-u", 0x78, 0x7f, 4},
		{"i16-s", 0x77, 0x7f, 3}, {"i16-u", 0x77, 0x7f, 4},
		{"v128", 0x7b, 0x7b, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, tc.storage, 0}, []byte{0x60, 1, 0x6d, 1, tc.result})),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0xfb, tc.op, 0, 0, 0x0b}))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["final-cast-scalar-get-fuse"]; got != 1 {
				t.Fatalf("fused checked field reads = %d; want 1 (rewrites: %v)", got, stats.Funcs[0].Peephole)
			}
			calls := stats.Funcs[0].Calls
			if native := calls["gcnative"] + calls["gcnative-leaf"]; native != 1 || calls[callKindHostSync] != 0 {
				t.Fatalf("cast/load calls=%v; want one native resolution and no Go helper", calls)
			}
		})
	}
}
