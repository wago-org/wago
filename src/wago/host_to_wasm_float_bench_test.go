//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func hostToWasmF64SignatureModule(params, results int) []byte {
	paramTypes := make([]wasm.ValType, params)
	resultTypes := make([]wasm.ValType, results)
	for i := range paramTypes {
		paramTypes[i] = wasm.F64
	}
	for i := range resultTypes {
		resultTypes[i] = wasm.F64
	}
	code := make([]byte, 0, results*2+1)
	for i := 0; i < results; i++ {
		code = append(code, 0x20, byte(i))
	}
	code = append(code, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(paramTypes, resultTypes))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(code))),
	)
}

func BenchmarkHostToWasmFloatSignatureMatrix(b *testing.B) {
	for _, shape := range [][2]int{{1, 1}, {2, 1}, {2, 2}, {4, 1}, {4, 4}} {
		params, results := shape[0], shape[1]
		b.Run(fmt.Sprintf("f64x%d-f64x%d", params, results), func(b *testing.B) {
			compiled := benchMustCompile(b, hostToWasmF64SignatureModule(params, results))
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("f")
			if err != nil {
				b.Fatal(err)
			}
			args := make([]uint64, params)
			for i := range args {
				args[i] = F64(float64(i) + 1.5)
			}
			check := func(got []uint64, err error) {
				b.Helper()
				if err != nil || len(got) != results {
					b.Fatalf("result = %v, %v", got, err)
				}
				for i := range got {
					if got[i] != args[i] {
						b.Fatalf("result[%d] = %x, want %x", i, got[i], args[i])
					}
				}
			}
			check(in.Invoke("f", args...))
			check(fn.Invoke(args...))
			b.Run("invoke", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchResultSink, err = in.Invoke("f", args...)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("prepared", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchResultSink, err = fn.Invoke(args...)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
