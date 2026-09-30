package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func BenchmarkSetGlobalValue(b *testing.B) {
	for _, tc := range []struct {
		name  string
		typ   wasm.ValType
		init  []byte
		value Value
	}{
		{"i32", wasm.I32, []byte{0x41, 0, 0x0b}, Value{typ: ValI32}},
		{"null_funcref", wasm.FuncRef, []byte{0xd0, 0x70, 0x0b}, Value{typ: ValFuncRef}},
		{"null_externref", wasm.ExternRef, []byte{0xd0, 0x6f, 0x0b}, Value{typ: ValExternRef}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c := benchMustCompile(b, wasmtest.Module(
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(tc.typ, true, tc.init))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0))),
			))
			in, err := Instantiate(c)
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := in.SetGlobalValue("g", tc.value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
