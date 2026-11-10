//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func tableOffsetModuleARM64(t testing.TB, tail bool) *wasm.Module {
	t.Helper()
	op := byte(0x11)
	if tail {
		op = 0x13
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, op, 0x00, 0x00, 0x0b}),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Compilation only: the table has one entry and generated code is never run.
func BenchmarkCompileIndirectTableOffsetARM64(b *testing.B) {
	for _, tc := range []struct {
		name     string
		tail     bool
		bindings []ImportBinding
	}{
		{"call", false, nil},
		{"tail", true, nil},
		{"tail-descriptor", true, []ImportBinding{}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			m := tableOffsetModuleARM64(b, tc.tail)
			opts := CompileOptions{ImportBindings: tc.bindings}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					_ = cm.CodeImage.Close()
				}
			}
		})
	}
}
