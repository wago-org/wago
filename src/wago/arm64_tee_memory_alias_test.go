//go:build (linux || darwin || windows) && arm64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestARM64TeeSinkPreservesDeferredLoadAddress(t *testing.T) {
	body := []byte{
		1, 1, 0x7f,
		0x20, 0, 0x45, 0x04, 0x40, 0x41, 0, 0x0f, 0x0b,
		0x20, 0, 0x28, 2, 116, 0x22, 1, 0x04, 0x40,
		0x20, 1, 0x20, 1, 0x28, 2, 0, 0x28, 2, 8, 0x11, 0, 0, 0x0f, 0x0b,
		0x20, 0, 0x28, 2, 104,
		0x20, 0, 0x28, 2, 100,
		0x6b, 0x41, 2, 0x75, 0x22, 0, 0x41, 0, 0x48,
		0x04, 0x40, 0x00, 0x0b, 0x20, 0, 0x0b,
	}
	segment := append([]byte{0, 0x41}, wasmtest.ULEB(1124)...)
	segment = append(segment, 0x0b, 8, 4, 0, 0, 0, 20, 0, 0, 0)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("count", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("count", I32(1024))
	if err != nil {
		t.Fatal(err)
	}
	if got := AsI32(result[0]); got != 4 {
		t.Fatalf("count = %d, want 4", got)
	}
}
