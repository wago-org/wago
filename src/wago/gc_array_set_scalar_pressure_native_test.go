//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCGeneralScalarArraySetLivePressure(t *testing.T) {
	params := make([]wasm.ValType, 17)
	for i := range params {
		params[i] = wasm.I64
	}
	results := append([]wasm.ValType(nil), params...)

	body := []byte{}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x23, 0, 0x41, 3)
	body = append(body, 0x20, 0, 0xfb, 14, 0)
	body = append(body, 0x0b)
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x50, 0, 0x5e, 0x7e, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, nil), wasmtest.FuncType(params, results), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3})), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("read", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0xfb, 6, 0, 0x24, 0, 0x0b}), wasmtest.Code(body), wasmtest.Code([]byte{0x23, 0, 0x20, 0, 0xfb, 11, 0, 0x0b}))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 17, 23); err != nil {
		t.Fatal(err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	args := make([]uint64, 17)
	for i := range args {
		args[i] = 0x123456789abc0000 + uint64(i)*1025
	}
	view := in.gc.NativeView()
	count := view.HandleCount
	for _, fallback := range []bool{false, true} {
		if fallback {
			view.HandleCount = 0
		}
		got, err := in.Invoke("run", args...)
		view.HandleCount = count
		if err != nil || len(got) != len(results) {
			t.Fatalf("fallback%v:%x,%v", fallback, got, err)
		}
		for i, want := range args {
			if got[i] != want {
				t.Fatalf("fallback%v prefix%d:%x,want%x", fallback, i, got[i], want)
			}
		}
		out, err := in.Invoke("read", 3)
		if err != nil || len(out) != 1 || out[0] != args[0] {
			t.Fatalf("stored value:%x,%v", out, err)
		}
	}
}
