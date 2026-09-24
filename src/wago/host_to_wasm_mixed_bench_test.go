//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func hostToWasmMixedIdentityModule(params []wasm.ValType, resultIndex int) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{params[resultIndex]}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, byte(resultIndex), 0x0b}))),
	)
}

func BenchmarkHostToWasmMixedSignatureMatrix(b *testing.B) {
	for _, tc := range []struct {
		name        string
		params      []wasm.ValType
		resultIndex int
		args        []uint64
	}{
		{"i32-f64-to-f64", []wasm.ValType{wasm.I32, wasm.F64}, 1, []uint64{I32(7), F64(2.5)}},
		{"i32-f64-to-i32", []wasm.ValType{wasm.I32, wasm.F64}, 0, []uint64{I32(7), F64(2.5)}},
		{"f64-i32-to-i32", []wasm.ValType{wasm.F64, wasm.I32}, 1, []uint64{F64(2.5), I32(7)}},
		{"i32-f32-f64-i64-to-f64", []wasm.ValType{wasm.I32, wasm.F32, wasm.F64, wasm.I64}, 2,
			[]uint64{I32(7), F32(1.5), F64(2.5), 0x1122334455667788}},
	} {
		b.Run(fmt.Sprint(tc.name), func(b *testing.B) {
			compiled := benchMustCompile(b, hostToWasmMixedIdentityModule(tc.params, tc.resultIndex))
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
			want := tc.args[tc.resultIndex]
			check := func(got []uint64, err error) {
				b.Helper()
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("result = %v, %v; want %x", got, err, want)
				}
			}
			check(in.Invoke("f", tc.args...))
			check(fn.Invoke(tc.args...))
			b.Run("invoke", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchResultSink, err = in.Invoke("f", tc.args...)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("prepared", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchResultSink, err = fn.Invoke(tc.args...)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkHostToWasmMixedPair(b *testing.B) {
	compiled := benchMustCompile(b, preparedMixedPairModule())
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
	args := []uint64{7, F64(2.5), 0x1122334455667788}
	for name, invoke := range map[string]func() ([]uint64, error){
		"invoke":   func() ([]uint64, error) { return in.Invoke("f", args...) },
		"prepared": func() ([]uint64, error) { return fn.Invoke(args...) },
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchResultSink, err = invoke()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
