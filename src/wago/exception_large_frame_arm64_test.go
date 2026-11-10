//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestExceptionLargeStackDisplacement(t *testing.T) {
	// The first handler record is at 16 + 2077*8 = 0x40f8.
	const locals = 2077
	body := append([]byte{1}, wasmtest.ULEB(locals)...)
	body = append(body, wasm.MustEncodeValType(wasm.I64))
	body = append(body, 0x42, 17, 0x21, 0, 0x42, 61, 0x21)
	body = append(body, wasmtest.ULEB(locals-1)...)
	body = append(body,
		0x02, 0x7f, // block (result i32)
		0x1f, 0x40, 1, 0, 0, 0, // try_table; catch tag 0 -> block
		0x41, 43, 0x08, 0, // throw tag 0 with payload 43
		0x0b, 0x00, 0x0b, // end try; unreachable; end block
		0x20, 0, 0xa7, 0x6a, // add first local
		0x20,
	)
	body = append(body, wasmtest.ULEB(locals-1)...)
	body = append(body, 0xa7, 0x6a, 0x0b) // add last local
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compiled, err := Compile(raw)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 121 {
		t.Fatalf("catch result = %v, want [121]", got)
	}
}
