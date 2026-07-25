//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"reflect"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// These tests port the portable call-boundary cases from Wasmtime's
// tests/all/func.rs at revision a5720e50d5ec9eab34eed690eee952abfdd0e3ba.
func TestWasmtimePortMultiResultCallBoundaries(t *testing.T) {
	t.Run("wasm to wasm", func(t *testing.T) {
		triple := wasmtest.FuncType(nil, []corewasm.ValType{corewasm.I32, corewasm.I32, corewasm.I32})
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(triple)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code([]byte{0x41, 0x01, 0x41, 0x02, 0x41, 0x03, 0x0b}),
				wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
			)),
		)
		in := instantiateWasmtimeAPIModule(t, mod, nil)
		assertWasmtimeAPIResults(t, in, "run", nil, []uint64{1, 2, 3})
	})

	t.Run("host to wasm", func(t *testing.T) {
		triple := wasmtest.FuncType(nil, []corewasm.ValType{corewasm.I32, corewasm.I32, corewasm.I32})
		imp := append(append(wasmtest.Name("host"), wasmtest.Name("triple")...), 0x00, 0x00)
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(triple)),
			wasmtest.Section(2, wasmtest.Vec(imp)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
		)
		in := instantiateWasmtimeAPIModule(t, mod, Imports{"host.triple": HostFunc(func(_ HostModule, _ []uint64, results []uint64) {
			results[0], results[1], results[2] = 1, 2, 3
		})})
		assertWasmtimeAPIResults(t, in, "run", nil, []uint64{1, 2, 3})
	})

	t.Run("array-style invocation", func(t *testing.T) {
		permute := wasmtest.FuncType(
			[]corewasm.ValType{corewasm.I32, corewasm.I32, corewasm.I32},
			[]corewasm.ValType{corewasm.I32, corewasm.I32, corewasm.I32},
		)
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(permute)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x02, 0x20, 0x00, 0x0b}))),
		)
		in := instantiateWasmtimeAPIModule(t, mod, nil)
		assertWasmtimeAPIResults(t, in, "run", []uint64{10, 20, 30}, []uint64{20, 30, 10})
	})
}

func TestWasmtimePortV128TypedCallBoundaries(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	one := wasmtest.FuncType([]corewasm.ValType{corewasm.V128}, []corewasm.ValType{corewasm.V128})
	two := wasmtest.FuncType(
		[]corewasm.ValType{corewasm.V128, corewasm.V128},
		[]corewasm.ValType{corewasm.V128, corewasm.V128},
	)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(one, two)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("a", 0, 0),
			wasmtest.ExportEntry("b", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}),
		)),
	)
	in := instantiateWasmtimeAPIModule(t, mod, nil)
	v1 := []uint64{0x0123456789abcdef, 0xfedcba9876543210}
	v2 := []uint64{0x1111222233334444, 0xaaaabbbbccccdddd}
	assertWasmtimeAPIResults(t, in, "a", v1, v1)
	assertWasmtimeAPIResults(t, in, "b", append(append([]uint64{}, v1...), v2...), append(append([]uint64{}, v1...), v2...))
}

func instantiateWasmtimeAPIModule(t *testing.T, mod []byte, imports Imports) *Instance {
	t.Helper()
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}

func assertWasmtimeAPIResults(t *testing.T, in *Instance, export string, args, want []uint64) {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err != nil {
		t.Fatalf("%s%v: %v", export, args, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s%v = %#x, want %#x", export, args, got, want)
	}
}
