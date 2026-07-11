//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// passiveDataModule and multiValueControlCallModule are shared by native
// platform tests. Keep these pure-binary fixtures independent of host tooling
// so Linux/arm64, Darwin/arm64, and TinyGo compile the same test surface.
func passiveDataModule() []byte {
	initBody := []byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x08, 0x00, 0x00, 0x0b}
	dropBody := []byte{0xfc, 0x09, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, append(wasmtest.ULEB(2), 0x00, 0x01)),
		wasmtest.Section(5, []byte{0x01, 0x00, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("drop", 0, 1), wasmtest.ExportEntry("mem", 2, 0))),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(initBody), wasmtest.Code(dropBody))),
		wasmtest.Section(11, wasmtest.Vec(append([]byte{0x01}, append(wasmtest.ULEB(5), []byte("hello")...)...))),
	)
}

func multiValueControlCallModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pair", 0, 0), wasmtest.ExportEntry("choose", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x12, 0x42, 0x13, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x04, 0x00, 0x10, 0x00, 0x05, 0x41, 0x07, 0x42, 0x09, 0x0b, 0x0b}),
		)),
	)
}
