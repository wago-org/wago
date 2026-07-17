//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func localI64GlobalTable64OffsetModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0x02, 0x02})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 0x01, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x23, 0x00, 0x0b, 0x01, 0x00})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x42, 0x01, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
}

func TestTable64ActiveOffsetUsesLocalImmutableI64Global(t *testing.T) {
	compiled, err := compileStagedTable64(localI64GlobalTable64OffsetModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("call")
	if err != nil || AsI32(got[0]) != 42 {
		t.Fatalf("call() = %v, %v; want 42", got, err)
	}
}
