package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPreparedTypedI32Calls(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("inc", 0, 0),
			wasmtest.ExportEntry("add", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}),
		)),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	inc, err := in.PrepareI32ToI32("inc")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := inc.Call(-2); err != nil || got != -1 {
		t.Fatalf("inc(-2) = %d, %v; want -1", got, err)
	}
	add, err := in.PrepareI32I32ToI32("add")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := add.Call(20, 22); err != nil || got != 42 {
		t.Fatalf("add(20, 22) = %d, %v; want 42", got, err)
	}
	if _, err := in.PrepareI32I32ToI32("inc"); err == nil || !strings.Contains(err.Error(), "expected signature") {
		t.Fatalf("mismatched typed prepare error = %v", err)
	}
	if _, err := (*PreparedI32ToI32)(nil).Call(0); err == nil {
		t.Fatal("nil typed prepared function was callable")
	}
}
