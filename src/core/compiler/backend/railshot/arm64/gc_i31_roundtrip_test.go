//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestI31RoundTripSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name string
		ops  []byte
		want int
	}{
		{"signed", []byte{0xfb, 28, 0xfb, 29}, 1},
		{"unsigned", []byte{0xfb, 28, 0xfb, 30}, 1},
		{"padded-get", []byte{0xfb, 28, 0xfb, 0x9e, 0}, 1},
		{"separated", []byte{0xfb, 28, 1, 0xfb, 30}, 0},
		{"different-consumer", []byte{0xfb, 28, 0xd1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0x20, 0}, tc.ops...)
			body = append(body, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer c.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["i31-roundtrip"]; got != tc.want {
				t.Fatalf("round-trip selections=%d, want%d", got, tc.want)
			}
		})
	}
}
