//go:build ((linux && amd64) || arm64) && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// passiveDataModule exports init(dst,src,n) = memory.init 0, drop = data.drop 0,
// and memory "mem", with a single passive data segment "hello". Broadly tagged so
// the bulk-memory / codec / snapshot tests that use it run on arm64 too (it was
// previously duplicated between a linux&&amd64 file and the darwin&&arm64 helper).
func passiveDataModule() []byte {
	initBody := []byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x08, 0x00, 0x00, 0x0b}
	dropBody := []byte{0xfc, 0x09, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, append(wasmtest.ULEB(2), 0x00, 0x01)),
		wasmtest.Section(5, []byte{0x01, 0x00, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 0),
			wasmtest.ExportEntry("drop", 0, 1),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(initBody), wasmtest.Code(dropBody))),
		wasmtest.Section(11, wasmtest.Vec(append([]byte{0x01}, append(wasmtest.ULEB(5), []byte("hello")...)...))),
	)
}
