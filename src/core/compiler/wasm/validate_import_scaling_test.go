package wasm_test

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func BenchmarkValidateImportCallScaling(b *testing.B) {
	for _, count := range []int{25000, 50000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			imp := append(wasmtest.Name("env"), wasmtest.Name("f")...)
			imp = append(imp, 0, 0)
			imports := make([][]byte, count)
			for i := range imports {
				imports[i] = imp
			}
			call := append([]byte{0x10}, wasmtest.ULEB(uint32(count-1))...)
			body := make([]byte, 0, len(call)*count+1)
			for range count {
				body = append(body, call...)
			}
			body = append(body, 0x0b)
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(2, wasmtest.Vec(imports...)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			module, err := wasm.DecodeModule(source)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := wasm.ValidateModule(module); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
