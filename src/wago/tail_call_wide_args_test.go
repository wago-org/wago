package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// wideCrossTailParamTypes fills the wrapper tail bank through slot 14 or 15.
// The EH tag-directory cell is immediately above that full 16-slot bank, so
// keeping this fixture shared makes both backends protect the exact boundary.
func wideCrossTailParamTypes(slots int) []wasm.ValType {
	params := make([]wasm.ValType, 0, slots)
	for range 8 {
		params = append(params, wasm.I64)
	}
	for range slots - 8 {
		params = append(params, wasm.F64)
	}
	return params
}

func wideCrossTailEHProducerModule(slots int) []byte {
	params := wideCrossTailParamTypes(slots)
	body := []byte{
		0x02, 0x40, // block: catch target
		0x1f, 0x40, 0x01, byte(wasm.CatchTag), 0x00, 0x00, // try_table void; catch tag 0 -> block
		0x08, 0x00, // throw tag 0
		0x0b, 0x00, // end try_table; unreachable normal continuation
		0x0b,       // end catch target
		0x20, 0x0e, // local.get 14
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(params, []wasm.ValType{wasm.F64}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pick", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func wideCrossTailFuncImport(module, name string, typeIdx uint32) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	entry = append(entry, byte(wasm.ExternFunc))
	return append(entry, wasmtest.ULEB(typeIdx)...)
}

func wideCrossTailConsumerModule(slots int, callRef bool) []byte {
	params := wideCrossTailParamTypes(slots)
	body := make([]byte, 0, slots*2+6)
	for i := range slots {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(i))...)
	}
	if callRef {
		body = append(body, 0xd2, 0x00, 0x15, 0x00) // ref.func 0; return_call_ref type 0
	} else {
		body = append(body, 0x12, 0x00) // return_call 0
	}
	body = append(body, 0x0b)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(2, wasmtest.Vec(wideCrossTailFuncImport("env", "pick", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
	}
	if callRef {
		declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(declared)))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	return wasmtest.Module(sections...)
}

func wideCrossTailArgs(slots int) []uint64 {
	args := make([]uint64, slots)
	for i := range args {
		args[i] = 0x3ff0000000000000 + uint64(i)*0x101
	}
	args[14] = 0x40c81cd6e631f8a1
	return args
}
