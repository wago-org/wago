//go:build linux && amd64 && !tinygo && wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDraglineSmallCountedLoopUnrollPreservesTripCounts(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 accumulator local
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // break when n == 0
		0x20, 0x01, 0x20, 0x00, 0x6a, 0x21, 0x01, // sum += n
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n--
		0x0c, 0x00, 0x0b, 0x0b, // continue; end loop/block
		0x20, 0x01, 0x0b, // return sum
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithBoundsChecks(BoundsChecksSignalsBased), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, n := range []uint32{0, 1, 2, 3, 4, 7, 8, 9, 15, 16, 17, 255} {
		result, err := instance.Invoke("sum", I32(int32(n)))
		want := int32(n * (n + 1) / 2)
		if err != nil || len(result) != 1 || AsI32(result[0]) != want {
			t.Fatalf("sum(%d) = %v, %v; want %d", n, result, err, want)
		}
	}
}
