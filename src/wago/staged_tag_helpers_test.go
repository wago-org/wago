//go:build ((linux && (amd64 || riscv64)) || ((linux || darwin) && arm64)) && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func stagedTagImportEntry(module, name string, typeIndex uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, byte(wasm.ExternTag), 0x00)
	return append(out, wasmtest.ULEB(typeIndex)...)
}

func stagedTagProductProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00}, []byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("primary", byte(wasm.ExternTag), 0),
			wasmtest.ExportEntry("alias", byte(wasm.ExternTag), 0),
			wasmtest.ExportEntry("other", byte(wasm.ExternTag), 1),
		)),
	)
}

func stagedTagProductConsumerModule(firstType, secondType uint32) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			stagedTagImportEntry("env", "first", firstType),
			stagedTagImportEntry("env", "second", secondType),
		)),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("reexport", byte(wasm.ExternTag), 0),
			wasmtest.ExportEntry("local", byte(wasm.ExternTag), 2),
		)),
	)
}
