//go:build linux && riscv64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func rv64WideDataSource(carrier byte, body []byte, extraTypes ...[]byte) []byte {
	physical := wasm.I32
	switch carrier {
	case 0x7e:
		physical = wasm.I64
	case 0x7d:
		physical = wasm.F32
	case 0x7c:
		physical = wasm.F64
	case 0x7b:
		physical = wasm.V128
	case 0x70:
		physical = wasm.FuncRef
	case 0x6f:
		physical = wasm.ExternRef
	}
	types := [][]byte{wasmtest.FuncType(nil, []wasm.ValType{physical}), wasmtest.FuncType([]wasm.ValType{physical}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{physical}, []wasm.ValType{physical})}
	types = append(types, extraTypes...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wide", "new1", 0), instructionFuncImport("wide", "new2", 0), instructionFuncImport("wide", "sum", 1), instructionFuncImport("wide", "id", 3))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 4))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}
func rv64PrepareWideData(t *testing.T, rt *Runtime, carrier byte, body []byte) *Module {
	t.Helper()
	mod, err := rt.Compile(rv64WideDataSource(carrier, body))
	if err != nil {
		t.Fatal(err)
	}
	return mod

}
