//go:build darwin && arm64

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// signExtModule exports f(i32)->i32 = i32.extend8_s(local0).
func signExtModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xc0, 0x0b}))),
	)
}

func tableTestBody(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return append(out, 0x0b)
}

func tableTestI32Const(v int32) []byte {
	out := []byte{0x41}
	out = append(out, wasmtest.SLEB32(v)...)
	return out
}

func tableTestLocalGet(i uint32) []byte {
	return append([]byte{0x20}, wasmtest.ULEB(i)...)
}

func tableTestRefFunc(i uint32) []byte {
	return append([]byte{0xd2}, wasmtest.ULEB(i)...)
}

func tableTestCallIndirect(typeIdx, tableIdx uint32) []byte {
	out := append([]byte{0x11}, wasmtest.ULEB(typeIdx)...)
	return append(out, wasmtest.ULEB(tableIdx)...)
}

func tableTestBulk(sub uint32, immediates ...uint32) []byte {
	out := append([]byte{0xfc}, wasmtest.ULEB(sub)...)
	for _, imm := range immediates {
		out = append(out, wasmtest.ULEB(imm)...)
	}
	return out
}

func tableTestFuncSection(typeIdxs ...uint32) []byte {
	items := make([][]byte, len(typeIdxs))
	for i, idx := range typeIdxs {
		items[i] = wasmtest.ULEB(idx)
	}
	return wasmtest.Section(3, wasmtest.Vec(items...))
}

func tableTestFuncIdxVec(funcs ...uint32) []byte {
	out := wasmtest.ULEB(uint32(len(funcs)))
	for _, f := range funcs {
		out = append(out, wasmtest.ULEB(f)...)
	}
	return out
}

func tableTestPassiveElem(funcs ...uint32) []byte {
	out := []byte{0x01, 0x00} // passive, elemkind funcref
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func tableTestDeclarativeElem(funcs ...uint32) []byte {
	out := []byte{0x03, 0x00} // declarative, elemkind funcref
	return append(out, tableTestFuncIdxVec(funcs...)...)
}
