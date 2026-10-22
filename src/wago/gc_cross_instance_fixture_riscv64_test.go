//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcAllocatingScalarProducerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	i32Type := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, i32Type)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("allocate", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
		)),
	)
}
