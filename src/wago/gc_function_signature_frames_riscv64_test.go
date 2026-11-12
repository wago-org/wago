//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64FunctionSignatureFrameSource(vector bool) []byte {
	body := []byte{0x20, 0, 0x41, 1, 0xfb, 0, 1, 0x1a, 0x14, 0, 0x0b}
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		body = append(append(prefix, 0x1a), body...)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f}, []byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x63, 0, 1, 0x7f})), wasmtest.Section(3, []byte{1, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func rv64FunctionSignatureProducerSource() []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f}, []byte{0x60, 0, 1, 0x63, 0})), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("function", 0, 1))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0xd2, 0, 0x0b}))))
}
