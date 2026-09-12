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
	}
}

func hostSignatureLoopModule(params, results int) []byte {
	importParams := make([]wasm.ValType, params)
	importResults := make([]wasm.ValType, results)
	for i := range importParams {
		importParams[i] = wasm.I32
	}
	for i := range importResults {
		importResults[i] = wasm.I32
	}
	body := []byte{
		1, 1, 0x7f, // local accumulator: i32
		0x02, 0x40, 0x03, 0x40, // block done; loop next
		0x20, 0, 0x45, 0x0d, 1, // count == 0: branch done
	}
	for i := 0; i < params; i++ {
		body = append(body, 0x41, byte(i+1)) // i32.const
	}
	body = append(body, 0x10, 0) // call import 0
	for i := 0; i < results; i++ {
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
			compiled, err := Compile(NewRuntimeConfig(), hostSignatureLoopModule(tc.params, tc.results))
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
			compiled, err := Compile(NewRuntimeConfig(), hostSignatureLoopModule(tc.params, tc.results))
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
						call.SetI32(i, int32(7+2*i))
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
	}
	fmt.Println(len(callbacks), "ordinary host signatures")
	// Output: 7 ordinary host signatures
}
