//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type hostSignatureCase struct {
	name            string
	params, results int
	valueType       wasm.ValType
	typed           any
}

func hostSignatureCases() []hostSignatureCase {
	return []hostSignatureCase{
		{name: "empty_to_empty", typed: func() {}},
		{name: "i32_to_empty", params: 1, typed: func(int32) {}},
		{name: "i32_to_i32", params: 1, results: 1, typed: func(int32) int32 { return 7 }},
		{name: "i32_i32_to_empty", params: 2, typed: func(int32, int32) {}},
		{name: "i32_i32_to_i32", params: 2, results: 1, typed: func(int32, int32) int32 { return 7 }},
		{name: "i32_to_i32_i32", params: 1, results: 2, typed: func(int32) (int32, int32) { return 7, 9 }},
		{name: "i32_i32_to_i32_i32", params: 2, results: 2, typed: func(int32, int32) (int32, int32) { return 7, 9 }},
		{name: "i64_to_i64", params: 1, results: 1, valueType: wasm.I64, typed: func(int64) int64 { return 7 }},
		{name: "i64_i64_to_i64", params: 2, results: 1, valueType: wasm.I64, typed: func(int64, int64) int64 { return 7 }},
		{name: "f32_to_f32", params: 1, results: 1, valueType: wasm.F32, typed: func(float32) float32 { return 7 }},
		{name: "f32_f32_to_f32", params: 2, results: 1, valueType: wasm.F32, typed: func(float32, float32) float32 { return 7 }},
		{name: "f64_to_f64", params: 1, results: 1, valueType: wasm.F64, typed: func(float64) float64 { return 7 }},
		{name: "f64_f64_to_f64", params: 2, results: 1, valueType: wasm.F64, typed: func(float64, float64) float64 { return 7 }},
	}
}

func hostSignatureLoopModule(params, results int, types ...wasm.ValType) []byte {
	valueType := wasm.I32
	if len(types) != 0 && types[0] != (wasm.ValType{}) {
		valueType = types[0]
	}
	importParams := make([]wasm.ValType, params)
	importResults := make([]wasm.ValType, results)
	for i := range importParams {
		importParams[i] = valueType
	}
	for i := range importResults {
		importResults[i] = valueType
	}
	body := []byte{
		1, 1, 0x7f, // local accumulator: i32
		0x02, 0x40, 0x03, 0x40, // block done; loop next
		0x20, 0, 0x45, 0x0d, 1, // count == 0: branch done
	}
	for i := 0; i < params; i++ {
		switch valueType {
		case wasm.I32:
			body = append(body, 0x41, byte(i+1))
		case wasm.I64:
			body = append(body, 0x42, byte(i+1))
		case wasm.F32:
			body = append(body, 0x43, 0, 0, 0, 0)
		case wasm.F64:
			body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0, 0)
		}
	}
	body = append(body, 0x10, 0) // call import 0
	for i := 0; i < results; i++ {
		switch valueType {
		case wasm.I64:
			body = append(body, 0xa7) // i32.wrap_i64
		case wasm.F32:
			body = append(body, 0xa8) // i32.trunc_f32_s
		case wasm.F64:
			body = append(body, 0xaa) // i32.trunc_f64_s
		}
		body = append(body, 0x21, 1) // consume result into accumulator
	}
	body = append(body,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0, // count--
		0x0c, 0, 0x0b, 0x0b, // branch next; end loop/block
		0x20, 1, 0x0b, // return accumulator
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(importParams, importResults),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestTypedHostSignatureMatrix(t *testing.T) {
	for _, tc := range hostSignatureCases() {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig(), hostSignatureLoopModule(tc.params, tc.results, tc.valueType))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, Imports{"env.f": tc.typed})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("run", I32(1))
			want := uint64(0)
			if tc.results != 0 {
				want = 7
			}
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("run = %v, %v; want %d", got, err, want)
			}
		})
	}
}

func TestMixedOriginalAndExpandedTypedHostSignatures(t *testing.T) {
	i32ToI32 := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	i32ToVoid := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	body := []byte{0, 0x20, 0, 0x10, 0, 0x20, 0, 0x10, 1, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(i32ToI32, i32ToVoid)),
		wasmtest.Section(2, wasmtest.Vec(
			importEntry("env", "transform", 0, 0),
			importEntry("env", "event", 0, 1),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig(), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	seen := int32(0)
	in, err := Instantiate(compiled, Imports{
		"env.transform": func(v int32) int32 { return v + 1 },
		"env.event":     func(v int32) { seen = v },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run", I32(41))
	if err != nil || len(got) != 1 || got[0] != 42 || seen != 41 {
		t.Fatalf("run = %v, %v; event %d; want [42], nil, 41", got, err, seen)
	}
}

func BenchmarkHostSignatureMatrix(b *testing.B) {
	const calls = int32(1024)
	for _, tc := range hostSignatureCases() {
		b.Run(tc.name, func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig(), hostSignatureLoopModule(tc.params, tc.results, tc.valueType))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			paths := []struct {
				name string
				fn   any
			}{
				{name: "generic", fn: CallerHostFunc(func(_ Caller, _ []uint64, results []uint64) {
					for i := range results {
						results[i] = uint64(7 + 2*i)
					}
				})},
				{name: "call", fn: HostCallFunc(func(call HostCall) {
					for i := 0; i < call.ResultCount(); i++ {
						switch tc.valueType {
						case wasm.I64:
							call.SetI64(i, int64(7+2*i))
						case wasm.F32:
							call.SetF32(i, float32(7+2*i))
						case wasm.F64:
							call.SetF64(i, float64(7+2*i))
						default:
							call.SetI32(i, int32(7+2*i))
						}
					}
				})},
				{name: "typed", fn: tc.typed},
			}
			if tc.params == 1 && tc.results == 0 {
				paths = append(paths, struct {
					name string
					fn   any
				}{name: "deferred", fn: I32HostEvent(func(int32) {})})
			}
			for _, path := range paths {
				b.Run(path.name, func(b *testing.B) {
					in, err := Instantiate(compiled, Imports{"env.f": path.fn})
					if err != nil {
						b.Fatal(err)
					}
					defer in.Close()
					if _, err := in.Invoke("run", I32(calls)); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := in.Invoke("run", I32(calls)); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(int64(b.N)*int64(calls)), "ns/call")
				})
			}
		})
	}
}

func ExampleHostCallFunc_signatureMatrix() {
	callbacks := []any{
		func() {},
		func(int32) {},
		func(v int32) int32 { return v },
		func(int32, int32) {},
		func(a, b int32) int32 { return a + b },
		func(v int32) (int32, int32) { return v, v },
		func(a, b int32) (int32, int32) { return a, b },
		func(v int64) int64 { return v },
		func(a, b int64) int64 { return a + b },
		func(v float32) float32 { return v },
		func(a, b float32) float32 { return a + b },
		func(v float64) float64 { return v },
		func(a, b float64) float64 { return a + b },
	}
	fmt.Println(len(callbacks), "ordinary host signatures")
	// Output: 13 ordinary host signatures
}
