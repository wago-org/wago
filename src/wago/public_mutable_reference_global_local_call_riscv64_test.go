//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MutableFuncrefLocalCallModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2),
			wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4),
			wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}),
			wasmtest.Code([]byte{0x41, 57, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalTailCallModule(source []byte) []byte {
	tail := slices.Clone(source)
	pattern := []byte{0x23, 0, 0x14, 0, 0x0b}
	i := bytes.LastIndex(tail, pattern)
	if i < 0 {
		panic("mutable call_ref body not found")
	}
	tail[i+2] = 0x15
	return tail
}

func rv64MutableFuncrefLocalCallScalarModule(typ wasm.ValType, targetBody []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2),
			wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4),
			wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code(targetBody),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalCallI32Module() []byte {
	return rv64MutableFuncrefLocalCallScalarModule(wasm.I32, []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b})
}

func rv64MutableFuncrefLocalTailCallI32Module() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x15, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalCallMixedModule() []byte {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x42, 1, 0x7c, 0x20, 2, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x20, 3, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalCallWideI32Module() []byte {
	types := make([]wasm.ValType, 9)
	identity, increment, call := []byte{}, []byte{}, []byte{}
	for i := byte(0); i < 9; i++ {
		types[i] = wasm.I32
		identity = append(identity, 0x20, i)
		increment = append(increment, 0x20, i, 0x41, 1, 0x6a)
		call = append(call, 0x20, i)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(append(identity, 0x0b)), wasmtest.Code(append(increment, 0x0b)),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code(append(call, 0x23, 0, 0x14, 0, 0x0b)),
		)),
	)
}

func rv64MutableFuncrefLocalCallVectorModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xfd, 77, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalTailCallVectorModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xfd, 77, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x15, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalCallMixedVectorModule() []byte {
	types := []wasm.ValType{wasm.I32, wasm.V128, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0xfd, 77, 0x20, 2, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalCallReferenceCarrierModule() []byte {
	params := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64}
	results := []wasm.ValType{wasm.ExternRef, wasm.FuncRef, wasm.I32, wasm.I64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 2, 0x20, 1, 0x20, 0, 0x20, 3, 0x0b}),
			wasmtest.Code([]byte{0x20, 2, 0x20, 1, 0x20, 0, 0x41, 1, 0x6a, 0x20, 3, 0x42, 1, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalTailCallReferenceCarrierModule() []byte {
	return rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallReferenceCarrierModule())
}

func rv64MutableFuncrefLocalCallHostReferenceCarrierModule() []byte {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x20, 5, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x20, 2, 0x20, 3, 0x42, 1, 0x7c, 0x20, 4, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x20, 5, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x20, 5, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalTailCallHostReferenceCarrierModule() []byte {
	return rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallHostReferenceCarrierModule())
}

func rv64MutableFuncrefLocalCallReferenceVectorCarrierModule() []byte {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.V128, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x20, 2, 0x20, 3, 0xfd, 77, 0x20, 4, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x23, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64MutableFuncrefLocalTailCallReferenceVectorCarrierModule() []byte {
	return rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallReferenceVectorCarrierModule())
}

func rv64MutableFuncrefLocalTailCallMixedVectorModule() []byte {
	types := []wasm.ValType{wasm.I32, wasm.V128, wasm.F64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{6, 0, 0, 1, 1, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0), wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_0", 0, 2), wasmtest.ExportEntry("set_1", 0, 3),
			wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("call", 0, 5),
			wasmtest.ExportEntry("cell", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0xfd, 77, 0x20, 2, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x23, 0, 0x15, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicMutableFuncrefGlobalLocalCall(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			cfg := rv64NativeTailConfig()
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-producer", rv64PublicReferenceFunctionCallBoundaryModule(51), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			foreignBits, err := producer.Invoke("get")
			if err != nil || len(foreignBits) != 1 || foreignBits[0] == 0 {
				t.Fatal("foreign producer token", foreignBits, err)
			}
			foreign := ValueOf(ValFuncRef, foreignBits[0])

			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call", rv64MutableFuncrefLocalCallModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalCall(view) || !view.usesGuestGlobalStoreHelper() {
				t.Fatalf("local-call witness missing: code=%d/%x entries=%v/%v refs=%t/%t/%t escape=%t globals=%+v values=%+v exports=%v/%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.nativeReferenceFunctionABI, view.nativeReferenceCallABI, view.nativeReferenceControlABI, view.dynamicFuncrefEscape, view.Globals, view.ValueTypes, view.Exports, view.GlobalExports, railshotValidateCompiled(view))
			}
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), module)
			module.Close()
			if err != nil {
				t.Fatal(err)
			}
			assertCall := func(want uint64) {
				t.Helper()
				got, invokeErr := consumer.Invoke("call")
				if invokeErr != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal("mutable funcref call_ref", got, invokeErr, "want", want)
				}
			}
			if got, invokeErr := consumer.Invoke("call"); got != nil {
				t.Fatal("null call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("null mutable funcref call trap", invokeErr)
				}
			}
			if _, err = consumer.Invoke("set_0"); err != nil {
				t.Fatal("install local zero", err)
			}
			assertCall(42)
			if _, err = consumer.Invoke("set_1"); err != nil {
				t.Fatal("install local one", err)
			}
			assertCall(57)

			before := producer.referenceLifetime().snapshot()
			if err = consumer.SetGlobalValue("cell", foreign); err != nil {
				t.Fatal("install foreign typed target", err)
			}
			installed := producer.referenceLifetime().snapshot()
			if installed.ResourceRoots != before.ResourceRoots+1 {
				t.Fatal("host ingress did not retain foreign call target", before, installed)
			}
			assertCall(51)
			if err = producer.Close(); err != nil {
				t.Fatal(err)
			}
			parked := producer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources {
				t.Fatal("typed cell did not retain closed foreign target", parked)
			}
			assertCall(51)
			if _, err = consumer.Invoke("set_0"); err != nil {
				t.Fatal("replace foreign target", err)
			}
			assertCall(42)
			if retired := producer.referenceLifetime().snapshot(); retired.ResourceRoots+1 != parked.ResourceRoots {
				t.Fatal("foreign target replacement did not retire exactly one cell root", parked, retired)
			}
			if _, err = consumer.Invoke("clear"); err != nil {
				t.Fatal("clear typed cell", err)
			}
			if got, invokeErr := consumer.Invoke("call"); got != nil {
				t.Fatal("cleared call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("cleared mutable funcref call trap", invokeErr)
				}
			}
			if err = producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err = consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err = compiled.Close(); err != nil {
				t.Fatal(err)
			}
			if err = rt.Close(); err != nil {
				t.Fatal(err)
			}
			if err = rt.WaitClosed(context.Background()); err != nil {
				t.Fatal(err)
			}
			if final := producer.referenceLifetime().snapshot(); final.PhysicalResources || final.ResourceRoots != 0 {
				t.Fatal("runtime token release did not retire foreign call target", final)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallI32(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()

			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-i32", rv64MutableFuncrefLocalCallI32Module(), artifact)
			if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
				t.Fatal("i32 local-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			assertTrap := func() {
				t.Helper()
				got, invokeErr := consumer.Invoke("call", 0xdeadbeef80000001)
				if got != nil {
					t.Fatal("null i32 call_ref returned a result", got, invokeErr)
				}
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("null i32 mutable funcref call trap", invokeErr)
				}
			}
			assertCall := func(input, want uint64) {
				t.Helper()
				got, invokeErr := consumer.Invoke("call", input)
				if invokeErr != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal("mutable i32 funcref call_ref", got, invokeErr, "want", want)
				}
			}
			assertTrap()
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install i32 identity", err)
			}
			assertCall(0xdeadbeef80000001, 0x80000001)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install i32 increment", err)
			}
			assertCall(0xcafebabeffffffff, 0)

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-i32-producer", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, false, true), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign i32 producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign i32 producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign i32 target", err)
			}
			assertCall(0xfeedface80000002, 0x80000002)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear i32 target", err)
			}
			assertTrap()

			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTailCallI32(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-i32", rv64MutableFuncrefLocalTailCallI32Module(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalCall(view) || !rv64HasReferenceTail(view) {
				t.Fatal("i32 local-tail-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			assertCall := func(want uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", 0xdeadbeef80000001)
				if err != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal("mutable return_call_ref", got, err, "want", want)
				}
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install tail identity", err)
			}
			assertCall(0x80000001)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install tail increment", err)
			}
			assertCall(0x80000002)

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-i32-producer", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, false, true), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign tail producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign tail producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign tail target", err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			assertCall(0x80000001)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear tail target", err)
			}
			if got, invokeErr := consumer.Invoke("call", 1); got != nil {
				t.Fatal("cleared return_call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("cleared mutable return_call_ref trap", invokeErr)
				}
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTailCallRemaining(t *testing.T) {
	wideTypes := make([]wasm.ValType, 9)
	wideInput := []uint64{
		0xdeadbeef80000000, 0xcafebabeffffffff, 0xfeedface00000001,
		0x111111117fffffff, 0x2222222280000001, 0x33333333fffffffe,
		0x4444444400000002, 0x55555555abcdef01, 0x6666666612345678,
	}
	wideIdentity, wideIncrement := make([]uint64, len(wideInput)), make([]uint64, len(wideInput))
	for i, value := range wideInput {
		wideTypes[i] = wasm.I32
		wideIdentity[i] = uint64(uint32(value))
		wideIncrement[i] = uint64(uint32(value) + 1)
	}
	tests := map[string]struct {
		source      []byte
		types       []wasm.ValType
		input       []uint64
		identity    []uint64
		transformed []uint64
		foreign     []uint64
	}{
		"zero": {
			rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallModule()), nil, nil,
			[]uint64{42}, []uint64{57}, []uint64{51},
		},
		"i64": {
			rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallScalarModule(wasm.I64, []byte{0x20, 0, 0x42, 1, 0x7c, 0x0b})), []wasm.ValType{wasm.I64}, []uint64{^uint64(0)},
			[]uint64{^uint64(0)}, []uint64{0}, []uint64{^uint64(0)},
		},
		"f32": {
			rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallScalarModule(wasm.F32, []byte{0x20, 0, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x0b})), []wasm.ValType{wasm.F32}, []uint64{0xdeadbeef00000000 | uint64(math.Float32bits(1.5))},
			[]uint64{uint64(math.Float32bits(1.5))}, []uint64{uint64(math.Float32bits(2.5))}, []uint64{uint64(math.Float32bits(1.5))},
		},
		"f64": {
			rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallScalarModule(wasm.F64, []byte{0x20, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b})), []wasm.ValType{wasm.F64}, []uint64{math.Float64bits(1.5)},
			[]uint64{math.Float64bits(1.5)}, []uint64{math.Float64bits(2.5)}, []uint64{math.Float64bits(1.5)},
		},
		"mixed": {
			rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallMixedModule()), []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64},
			[]uint64{0xdeadbeef80000001, ^uint64(0), 0xfeedface00000000 | uint64(math.Float32bits(1.5)), math.Float64bits(1.5)},
			[]uint64{0x80000001, ^uint64(0), uint64(math.Float32bits(1.5)), math.Float64bits(1.5)},
			[]uint64{0x80000002, 0, uint64(math.Float32bits(2.5)), math.Float64bits(2.5)},
			[]uint64{0x80000001, ^uint64(0), uint64(math.Float32bits(1.5)), math.Float64bits(1.5)},
		},
		"wide-i32": {
			rv64MutableFuncrefLocalTailCallModule(rv64MutableFuncrefLocalCallWideI32Module()), wideTypes, wideInput, wideIdentity, wideIncrement, wideIdentity,
		},
	}
	for name, test := range tests {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", name, artifact), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rt.Close()
				compiled := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-"+name, test.source, artifact)
				view := compiled.executionView()
				if !rv64MutableFuncrefGlobalLocalCall(view) || !rv64HasReferenceTail(view) {
					t.Fatal("local-tail-call witness missing", name)
				}
				consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
				assertCall := func(want []uint64) {
					t.Helper()
					got, err := consumer.Invoke("call", test.input...)
					if err != nil || !slices.Equal(got, want) {
						t.Fatal("mutable return_call_ref", name, got, err, "want", want)
					}
				}
				if _, err := consumer.Invoke("set_0"); err != nil {
					t.Fatal("install tail identity", name, err)
				}
				assertCall(test.identity)
				if _, err := consumer.Invoke("set_1"); err != nil {
					t.Fatal("install tail transform", name, err)
				}
				assertCall(test.transformed)

				var producerCode *Compiled
				var producer *Instance
				var token uint64
				if name == "zero" {
					producerCode = rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-zero-producer", rv64PublicReferenceFunctionCallBoundaryModule(51), artifact)
					producer = rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
					bits, err := producer.Invoke("get")
					if err != nil || len(bits) != 1 || bits[0] == 0 {
						t.Fatal("foreign zero tail producer token", bits, err)
					}
					token = bits[0]
				} else {
					producerCode = rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-"+name+"-producer", rv64PublicReferenceTailModule(test.types, false, true), artifact)
					producer = rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
					descriptor, ok := producer.localFuncrefDescriptor(0)
					if !ok {
						t.Fatal("foreign tail producer descriptor", name)
					}
					var err error
					token, err = rt.refStore.issue(producer, descriptor)
					if err != nil {
						t.Fatal("foreign tail producer token", name, err)
					}
				}
				if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
					t.Fatal("install foreign tail target", name, err)
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				assertCall(test.foreign)
				if _, err := consumer.Invoke("clear"); err != nil {
					t.Fatal("clear tail target", name, err)
				}
				if got, invokeErr := consumer.Invoke("call", test.input...); got != nil {
					t.Fatal("cleared return_call_ref returned a result", name, got, invokeErr)
				} else {
					var trap *coreruntime.TrapError
					if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
						t.Fatal("cleared mutable return_call_ref trap", name, invokeErr)
					}
				}
				if err := producerCode.Close(); err != nil {
					t.Fatal(err)
				}
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallScalars(t *testing.T) {
	tests := map[string]struct {
		typ       wasm.ValType
		body      []byte
		input     uint64
		identity  uint64
		increment uint64
	}{
		"i64": {wasm.I64, []byte{0x20, 0, 0x42, 1, 0x7c, 0x0b}, ^uint64(0), ^uint64(0), 0},
		"f32": {wasm.F32, []byte{0x20, 0, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x0b}, 0xdeadbeef00000000 | uint64(math.Float32bits(1.5)), uint64(math.Float32bits(1.5)), uint64(math.Float32bits(2.5))},
		"f64": {wasm.F64, []byte{0x20, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}, math.Float64bits(1.5), math.Float64bits(1.5), math.Float64bits(2.5)},
	}
	for name, test := range tests {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", name, artifact), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rt.Close()
				compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-"+name, rv64MutableFuncrefLocalCallScalarModule(test.typ, test.body), artifact)
				if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
					t.Fatal("scalar local-call witness missing")
				}
				consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
				assertCall := func(want uint64) {
					t.Helper()
					got, err := consumer.Invoke("call", test.input)
					if err != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatal("mutable scalar funcref call_ref", got, err, "want", want)
					}
				}
				if _, err := consumer.Invoke("set_0"); err != nil {
					t.Fatal("install scalar identity", err)
				}
				assertCall(test.identity)
				if _, err := consumer.Invoke("set_1"); err != nil {
					t.Fatal("install scalar arithmetic target", err)
				}
				assertCall(test.increment)

				producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-"+name+"-producer", rv64PublicReferenceTailModule([]wasm.ValType{test.typ}, false, true), artifact)
				producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
				descriptor, ok := producer.localFuncrefDescriptor(0)
				if !ok {
					t.Fatal("foreign scalar producer descriptor")
				}
				token, err := rt.refStore.issue(producer, descriptor)
				if err != nil {
					t.Fatal("foreign scalar producer token", err)
				}
				if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
					t.Fatal("install foreign scalar target", err)
				}
				assertCall(test.identity)
				if _, err := consumer.Invoke("clear"); err != nil {
					t.Fatal("clear scalar target", err)
				}
				if got, invokeErr := consumer.Invoke("call", test.input); got != nil {
					t.Fatal("cleared scalar call_ref returned a result", got, invokeErr)
				} else {
					var trap *coreruntime.TrapError
					if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
						t.Fatal("cleared scalar mutable funcref call trap", invokeErr)
					}
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := producerCode.Close(); err != nil {
					t.Fatal(err)
				}
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallMixed(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-mixed", rv64MutableFuncrefLocalCallMixedModule(), artifact)
			if view := compiled.executionView(); !rv64MutableFuncrefGlobalLocalCall(view) {
				t.Fatalf("mixed local-call witness missing: code=%d/%x entries=%v/%v validation=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, railshotValidateCompiled(view))
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			input := []uint64{0xdeadbeef80000001, ^uint64(0), 0xfeedface00000000 | uint64(math.Float32bits(1.5)), math.Float64bits(1.5)}
			identity := []uint64{0x80000001, ^uint64(0), uint64(math.Float32bits(1.5)), math.Float64bits(1.5)}
			increment := []uint64{0x80000002, 0, uint64(math.Float32bits(2.5)), math.Float64bits(2.5)}
			assertCall := func(want []uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatal("mutable mixed funcref call_ref", got, err, "want", want)
				}
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install mixed identity", err)
			}
			assertCall(identity)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install mixed arithmetic target", err)
			}
			assertCall(increment)

			types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-mixed-producer", rv64PublicReferenceTailModule(types, false, true), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign mixed producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign mixed producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign mixed target", err)
			}
			assertCall(identity)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear mixed target", err)
			}
			if got, invokeErr := consumer.Invoke("call", input...); got != nil {
				t.Fatal("cleared mixed call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("cleared mixed mutable funcref call trap", invokeErr)
				}
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallWideI32(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-wide-i32", rv64MutableFuncrefLocalCallWideI32Module(), artifact)
			if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
				t.Fatal("wide i32 local-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			input := []uint64{
				0xdeadbeef80000000, 0xcafebabeffffffff, 0xfeedface00000001,
				0x111111117fffffff, 0x2222222280000001, 0x33333333fffffffe,
				0x4444444400000002, 0x55555555abcdef01, 0x6666666612345678,
			}
			identity, increment := make([]uint64, len(input)), make([]uint64, len(input))
			for i, value := range input {
				identity[i] = uint64(uint32(value))
				increment[i] = uint64(uint32(value) + 1)
			}
			assertCall := func(want []uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatal("wide i32 mutable call_ref", got, err, "want", want)
				}
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install wide i32 identity", err)
			}
			assertCall(identity)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install wide i32 increment", err)
			}
			assertCall(increment)

			types := make([]wasm.ValType, len(input))
			for i := range types {
				types[i] = wasm.I32
			}
			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-wide-i32-producer", rv64PublicReferenceTailModule(types, false, true), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign wide i32 producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign wide i32 producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign wide i32 target", err)
			}
			assertCall(identity)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear wide i32 target", err)
			}
			if got, invokeErr := consumer.Invoke("call", input...); got != nil {
				t.Fatal("cleared wide i32 call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("cleared wide i32 mutable funcref call trap", invokeErr)
				}
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallVector(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-vector", rv64MutableFuncrefLocalCallVectorModule(), artifact)
			if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
				t.Fatal("vector local-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			input := []uint64{0x0123456789abcdef, 0xfedcba9876543210}
			inverted := []uint64{^input[0], ^input[1]}
			assertCall := func(want []uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatal("vector mutable call_ref", got, err, "want", want)
				}
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install vector identity", err)
			}
			assertCall(input)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install vector not target", err)
			}
			assertCall(inverted)

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-vector-producer", rv64PublicReferenceTailModule([]wasm.ValType{wasm.V128}, false, true), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign vector producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign vector producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign vector target", err)
			}
			assertCall(input)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear vector target", err)
			}
			if got, invokeErr := consumer.Invoke("call", input...); got != nil {
				t.Fatal("cleared vector call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("cleared vector mutable funcref call trap", invokeErr)
				}
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTailCallVector(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-vector", rv64MutableFuncrefLocalTailCallVectorModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalCall(view) || !rv64HasReferenceTail(view) {
				t.Fatal("vector local-tail-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			input := []uint64{0x0123456789abcdef, 0xfedcba9876543210}
			inverted := []uint64{^input[0], ^input[1]}
			assertCall := func(want []uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatal("vector mutable return_call_ref", got, err, "want", want)
				}
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install vector tail identity", err)
			}
			assertCall(input)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install vector tail not target", err)
			}
			assertCall(inverted)

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-vector-producer", rv64PublicReferenceTailModule([]wasm.ValType{wasm.V128}, false, true), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign vector tail producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign vector tail producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign vector tail target", err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			assertCall(input)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear vector tail target", err)
			}
			if got, invokeErr := consumer.Invoke("call", input...); got != nil {
				t.Fatal("cleared vector return_call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("cleared vector mutable return_call_ref trap", invokeErr)
				}
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallMixedVector(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-mixed-vector", rv64MutableFuncrefLocalCallMixedVectorModule(), artifact)
			if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
				t.Fatal("mixed vector local-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			input := []uint64{0xdeadbeef80000001, 0x0123456789abcdef, 0xfedcba9876543210, math.Float64bits(2.5)}
			identity := []uint64{0x80000001, input[1], input[2], input[3]}
			transformed := []uint64{0x80000002, ^input[1], ^input[2], math.Float64bits(3.5)}
			assertCall := func(want []uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatal("mixed vector mutable call_ref", got, err, "want", want)
				}
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install mixed vector identity", err)
			}
			assertCall(identity)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install mixed vector transform", err)
			}
			assertCall(transformed)

			types := []wasm.ValType{wasm.I32, wasm.V128, wasm.F64}
			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-mixed-vector-producer", rv64PublicReferenceTailModule(types, false, true), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign mixed vector producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign mixed vector producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign mixed vector target", err)
			}
			assertCall(identity)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear mixed vector target", err)
			}
			if got, invokeErr := consumer.Invoke("call", input...); got != nil {
				t.Fatal("cleared mixed vector call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("cleared mixed vector mutable funcref call trap", invokeErr)
				}
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallReferenceCarriers(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-reference-carriers", rv64MutableFuncrefLocalCallReferenceCarrierModule(), artifact)
			view := compiled.executionView()
			profile, target0, target1 := rv64MutableFuncrefGlobalLocalCall(view), rv64MutableReferenceCarrierTarget(view, 0), rv64MutableReferenceCarrierTarget(view, 1)
			if !profile || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != 19 || !target0 || !target1 {
				t.Fatal("reference-carrier local-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			defer consumer.Close()
			ref, err := rt.NewExternRef(struct{ Name string }{"mutable-carrier"})
			if err != nil {
				t.Fatal(err)
			}
			extern := ValueExternRef(ref).Bits()
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install reference-carrier identity", err)
			}
			cell, err := consumer.GlobalValue("cell")
			if err != nil || cell.FuncRef().IsNull() {
				t.Fatal("reference-carrier argument token", cell, err)
			}
			argument := cell.Bits()
			input := []uint64{0xdeadbeef80000001, argument, extern, ^uint64(0)}
			assertCall := func(wantI32, wantI64 uint64, wantTarget int) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || len(got) != 4 || got[0] != extern || got[2] != wantI32 || got[3] != wantI64 || !consumer.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[1]).FuncRef(), uint32(wantTarget)) {
					t.Fatal("mutable reference-carrier call_ref", got, err, "want", wantI32, wantI64, wantTarget)
				}
				rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			}
			assertCall(0x80000001, ^uint64(0), 0)
			typed, err := consumer.InvokeValues(context.Background(), "call", ValueI32(-2147483647), ValueFuncRef(cell.FuncRef()), ValueExternRef(ref), ValueI64(-1))
			if err != nil || len(typed) != 4 || typed[0].Type() != ValExternRef || typed[0].ExternRef() != ref || typed[1].Type() != ValFuncRef || !consumer.FuncRefMatchesFunction(typed[1].FuncRef(), 0) || typed[2].Type() != ValI32 || typed[2].Bits() != 0x80000001 || typed[3].Type() != ValI64 || typed[3].Bits() != ^uint64(0) {
				t.Fatal("typed mutable reference-carrier call_ref", typed, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install reference-carrier transform", err)
			}
			assertCall(0x80000002, 0, 0)

			producer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign reference-carrier target descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign reference-carrier target token", err)
			}
			tokenHeld := producer.referenceLifetime().snapshot()
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign reference-carrier target", err)
			}
			installed := producer.referenceLifetime().snapshot()
			if installed.ResourceRoots != tokenHeld.ResourceRoots+1 || !installed.PhysicalResources || installed.LogicallyClosed {
				t.Fatal("reference-carrier cell did not retain foreign target", tokenHeld, installed)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			parked := producer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != installed.ResourceRoots {
				t.Fatal("reference-carrier cell did not park closed foreign target", installed, parked)
			}
			assertCall(0x80000001, ^uint64(0), 0)
			if producer.usesIndependentExecution() || consumer.usesIndependentExecution() || !producer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign mutable reference-carrier owners did not reconcile shared execution")
			}
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear reference-carrier target", err)
			}
			cleared := producer.referenceLifetime().snapshot()
			if !cleared.LogicallyClosed || !cleared.PhysicalResources || cleared.ResourceRoots+1 != parked.ResourceRoots {
				t.Fatal("reference-carrier clear did not retire exactly one cell root", parked, cleared)
			}
			if got, invokeErr := consumer.Invoke("call", input...); got != nil {
				t.Fatal("cleared reference-carrier call_ref returned values", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("cleared reference-carrier call_ref trap", invokeErr)
				}
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("mutable reference-carrier externref remained pinned")
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.WaitClosed(context.Background()); err != nil {
				t.Fatal(err)
			}
			if final := producer.referenceLifetime().snapshot(); final.PhysicalResources || final.ResourceRoots != 0 {
				t.Fatal("runtime token release did not retire foreign reference-carrier target", final)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTailCallReferenceCarriers(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-reference-carriers", rv64MutableFuncrefLocalTailCallReferenceCarrierModule(), artifact)
			view := compiled.executionView()
			m := rv64NativeTailModule(view)
			if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != 20 || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) || m == nil || len(m.Plans) != 1 || !rv64MutableReferenceCarrierTailPlan(view, m.Plans[0]) {
				t.Fatal("reference-carrier local-tail-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			ref, err := rt.NewExternRef(struct{ Name string }{"mutable-tail-carrier"})
			if err != nil {
				t.Fatal(err)
			}
			extern := ValueExternRef(ref).Bits()
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install tail reference-carrier identity", err)
			}
			cell, err := consumer.GlobalValue("cell")
			if err != nil || cell.FuncRef().IsNull() {
				t.Fatal("tail reference-carrier argument token", cell, err)
			}
			argument := cell.Bits()
			input := []uint64{0xdeadbeef80000001, argument, extern, ^uint64(0)}
			assertCall := func(wantI32, wantI64 uint64, wantTarget int) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || len(got) != 4 || got[0] != extern || got[2] != wantI32 || got[3] != wantI64 || !consumer.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[1]).FuncRef(), uint32(wantTarget)) {
					t.Fatal("mutable reference-carrier return_call_ref", got, err, "want", wantI32, wantI64, wantTarget)
				}
				rv64AssertPublicReferenceCarrierStateClear(t, consumer)
			}
			assertCall(0x80000001, ^uint64(0), 0)
			typed, err := consumer.InvokeValues(context.Background(), "call", ValueI32(-2147483647), ValueFuncRef(cell.FuncRef()), ValueExternRef(ref), ValueI64(-1))
			if err != nil || len(typed) != 4 || typed[0].Type() != ValExternRef || typed[0].ExternRef() != ref || typed[1].Type() != ValFuncRef || !consumer.FuncRefMatchesFunction(typed[1].FuncRef(), 0) || typed[2].Type() != ValI32 || typed[2].Bits() != 0x80000001 || typed[3].Type() != ValI64 || typed[3].Bits() != ^uint64(0) {
				t.Fatal("typed mutable reference-carrier return_call_ref", typed, err)
			}
			rv64AssertPublicReferenceCarrierStateClear(t, consumer)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install tail reference-carrier transform", err)
			}
			assertCall(0x80000002, 0, 0)

			hostCarrierArgumentCode := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-reference-carriers-host-carrier-argument", rv64MutableFuncrefLocalCallHostReferenceCarrierModule(), artifact)
			hostCarrierArgumentProducer := rv64NativeTokenInstantiate(t, rt, hostCarrierArgumentCode, NewImports())
			hostCarrierArgumentDescriptor, ok := hostCarrierArgumentProducer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign host-reference-carrier argument descriptor")
			}
			hostCarrierArgumentToken, err := rt.refStore.issue(hostCarrierArgumentProducer, hostCarrierArgumentDescriptor)
			if err != nil {
				t.Fatal("foreign host-reference-carrier argument token", err)
			}
			hostCarrierArgumentHeld := hostCarrierArgumentProducer.referenceLifetime().snapshot()
			if err := hostCarrierArgumentProducer.Close(); err != nil {
				t.Fatal(err)
			}
			hostCarrierArgumentParked := hostCarrierArgumentProducer.referenceLifetime().snapshot()
			if !hostCarrierArgumentParked.LogicallyClosed || !hostCarrierArgumentParked.PhysicalResources || hostCarrierArgumentParked.ResourceRoots != hostCarrierArgumentHeld.ResourceRoots {
				t.Fatal("foreign host-reference-carrier argument did not park closed producer", hostCarrierArgumentHeld, hostCarrierArgumentParked)
			}
			hostCarrierInput := slices.Clone(input)
			hostCarrierInput[1] = hostCarrierArgumentToken
			got, err := consumer.Invoke("call", hostCarrierInput...)
			if err != nil || len(got) != 4 || got[0] != extern || got[1] != hostCarrierArgumentToken || got[2] != 0x80000002 || got[3] != 0 {
				t.Fatal("foreign host-reference-carrier argument round trip", got, err)
			}
			rv64AssertPublicReferenceCarrierStateClear(t, consumer)
			if afterArgument := hostCarrierArgumentProducer.referenceLifetime().snapshot(); !afterArgument.LogicallyClosed || !afterArgument.PhysicalResources || afterArgument.ResourceRoots != hostCarrierArgumentParked.ResourceRoots {
				t.Fatal("foreign host-reference-carrier argument scope did not retire exactly", hostCarrierArgumentParked, afterArgument)
			}
			if hostCarrierArgumentProducer.usesIndependentExecution() || consumer.usesIndependentExecution() || !hostCarrierArgumentProducer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign host-reference-carrier argument owners did not reconcile shared execution")
			}

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-reference-carriers-ordinary-producer", rv64MutableFuncrefLocalCallReferenceCarrierModule(), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(1)
			if !ok {
				t.Fatal("foreign ordinary reference-carrier descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign ordinary reference-carrier token", err)
			}
			tokenHeld := producer.referenceLifetime().snapshot()
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			parkedToken := producer.referenceLifetime().snapshot()
			if !parkedToken.LogicallyClosed || !parkedToken.PhysicalResources || parkedToken.ResourceRoots != tokenHeld.ResourceRoots {
				t.Fatal("foreign argument token did not park closed producer", tokenHeld, parkedToken)
			}
			foreignInput := slices.Clone(input)
			foreignInput[1] = token
			got, err = consumer.Invoke("call", foreignInput...)
			if err != nil || len(got) != 4 || got[0] != extern || got[1] != token || got[2] != 0x80000002 || got[3] != 0 {
				t.Fatal("foreign mutable reference-carrier argument round trip", got, err)
			}
			rv64AssertPublicReferenceCarrierStateClear(t, consumer)
			if afterArgument := producer.referenceLifetime().snapshot(); !afterArgument.LogicallyClosed || !afterArgument.PhysicalResources || afterArgument.ResourceRoots != parkedToken.ResourceRoots {
				t.Fatal("foreign argument scope did not retire exactly", parkedToken, afterArgument)
			}
			if producer.usesIndependentExecution() || consumer.usesIndependentExecution() || !producer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign reference-carrier argument owners did not reconcile shared execution")
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign ordinary reference-carrier tail target", err)
			}
			installed := producer.referenceLifetime().snapshot()
			if installed.ResourceRoots != parkedToken.ResourceRoots+1 || !installed.PhysicalResources || !installed.LogicallyClosed {
				t.Fatal("tail reference-carrier cell did not retain closed foreign target", parkedToken, installed)
			}
			assertCall(0x80000002, 0, 0)
			if producer.usesIndependentExecution() || consumer.usesIndependentExecution() || !producer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign mutable tail reference-carrier owners did not reconcile shared execution")
			}
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear tail reference-carrier target", err)
			}
			cleared := producer.referenceLifetime().snapshot()
			if !cleared.LogicallyClosed || !cleared.PhysicalResources || cleared.ResourceRoots+1 != installed.ResourceRoots {
				t.Fatal("tail reference-carrier clear did not retire exactly one cell root", installed, cleared)
			}
			if got, invokeErr := consumer.Invoke("call", input...); got != nil {
				t.Fatal("cleared reference-carrier return_call_ref returned values", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("cleared reference-carrier return_call_ref trap", invokeErr)
				}
			}
			rv64AssertPublicReferenceCarrierStateClear(t, consumer)
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("mutable tail reference-carrier externref remained pinned")
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := hostCarrierArgumentCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.WaitClosed(context.Background()); err != nil {
				t.Fatal(err)
			}
			if final := producer.referenceLifetime().snapshot(); final.PhysicalResources || final.ResourceRoots != 0 {
				t.Fatal("runtime token release did not retire foreign tail reference-carrier target", final)
			}
			if final := hostCarrierArgumentProducer.referenceLifetime().snapshot(); final.PhysicalResources || final.ResourceRoots != 0 {
				t.Fatal("runtime token release did not retire foreign host-reference-carrier argument", final)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallHostReferenceCarriers(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-host-reference-carriers", rv64MutableFuncrefLocalCallHostReferenceCarrierModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != 21 || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) || rv64HasReferenceTail(view) {
				t.Fatal("host-reference-carrier local-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			ref, err := rt.NewExternRef("mutable-host-reference-carrier")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install host-reference-carrier identity", err)
			}
			cell, err := consumer.GlobalValue("cell")
			if err != nil || cell.FuncRef().IsNull() {
				t.Fatal("host-reference-carrier argument token", cell, err)
			}
			argumentRoot, err := rt.NewFuncRefGlobal(cell.FuncRef(), false)
			if err != nil {
				t.Fatal("retain host-reference-carrier funcref argument", err)
			}
			defer argumentRoot.Close()
			input := []uint64{0xdeadbeef80000001, cell.Bits(), ValueExternRef(ref).Bits(), ^uint64(0), 0xfeedface40000000, math.Float64bits(2.5)}
			assertCall := func(wantI32, wantI64, wantF32, wantF64 uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || len(got) != 6 || got[0] != wantI32 || got[1] != input[1] || got[2] != input[2] || got[3] != wantI64 || got[4] != wantF32 || got[5] != wantF64 {
					t.Fatal("mutable host-reference-carrier call_ref", got, err)
				}
				rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			}
			assertCall(0x80000001, ^uint64(0), uint64(math.Float32bits(2)), math.Float64bits(2.5))
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install host-reference-carrier transform", err)
			}
			assertCall(0x80000002, 0, uint64(math.Float32bits(3)), math.Float64bits(3.5))

			calls := 0
			host := HostCallFunc(func(call HostCall) {
				calls++
				if uint32(call.I32(0)) != 0x80000001 || call.FuncRef(1).IsNull() || call.ExternRef(2) != ref || call.I64(3) != -1 || math.Float32bits(call.F32(4)) != math.Float32bits(2) || math.Float64bits(call.F64(5)) != math.Float64bits(2.5) {
					panic("mutable host-reference-carrier arguments")
				}
				call.ResultSlots()[0] = 0xfeedface00000000 | uint64(uint32(call.I32(0))+5)
				call.SetFuncRef(1, call.FuncRef(1))
				call.SetExternRef(2, call.ExternRef(2))
				call.SetI64(3, call.I64(3)+5)
				call.ResultSlots()[4] = 0xfeedface00000000 | uint64(math.Float32bits(call.F32(4)+5))
				call.SetF64(5, call.F64(5)+5)
			})
			types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-host-reference-carriers-producer", rv64PublicReferenceTailModule(types, true, true), artifact)
			owner, err := rt.NewHostFuncRef(host, FuncSig{Params: []ValType{ValI32, ValFuncRef, ValExternRef, ValI64, ValF32, ValF64}, Results: []ValType{ValI32, ValFuncRef, ValExternRef, ValI64, ValF32, ValF64}})
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			producer := rv64HostTailInstantiate(t, rt, producerCode, NewImports().Function("env", "target", owner))
			if !rv64PublicDynamicHostReferenceBoundary(producer.c) || len(producer.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
				t.Fatal("dynamic host-reference-carrier producer witness")
			}
			descriptor := uint64(offHeapSlicePtr(producer.funcRefDescs)) + coreruntime.FuncRefDescBytes
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("dynamic host-reference-carrier token", err)
			}
			tokenHeld := producer.referenceLifetime().snapshot()
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install dynamic host-reference-carrier target", err)
			}
			installed := producer.referenceLifetime().snapshot()
			if installed.ResourceRoots != tokenHeld.ResourceRoots+1 {
				t.Fatal("dynamic host-reference-carrier cell root", tokenHeld, installed)
			}
			assertCall(0x80000006, 4, uint64(math.Float32bits(7)), math.Float64bits(7.5))
			if calls != 1 {
				t.Fatal("dynamic host-reference-carrier call count", calls)
			}
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear dynamic host-reference-carrier target", err)
			}
			if cleared := producer.referenceLifetime().snapshot(); cleared.ResourceRoots+1 != installed.ResourceRoots {
				t.Fatal("dynamic host-reference-carrier clear root", installed, cleared)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("mutable host-reference-carrier externref remained pinned")
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTailCallHostReferenceCarriers(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-host-reference-carriers", rv64MutableFuncrefLocalTailCallHostReferenceCarrierModule(), artifact)
			view := compiled.executionView()
			m := rv64NativeTailModule(view)
			if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != 22 || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) || m == nil || len(m.Plans) != 1 || !rv64MutableReferenceCarrierTailPlan(view, m.Plans[0]) {
				t.Fatal("host-reference-carrier local-tail-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			ref, err := rt.NewExternRef("mutable-tail-host-reference-carrier")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install tail host-reference-carrier identity", err)
			}
			cell, err := consumer.GlobalValue("cell")
			if err != nil || cell.FuncRef().IsNull() {
				t.Fatal("tail host-reference-carrier argument token", cell, err)
			}
			argumentRoot, err := rt.NewFuncRefGlobal(cell.FuncRef(), false)
			if err != nil {
				t.Fatal("retain tail host-reference-carrier funcref argument", err)
			}
			defer argumentRoot.Close()
			input := []uint64{0xdeadbeef80000001, cell.Bits(), ValueExternRef(ref).Bits(), ^uint64(0), 0xfeedface40000000, math.Float64bits(2.5)}
			assertCall := func(wantI32, wantI64, wantF32, wantF64 uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || len(got) != 6 || got[0] != wantI32 || got[1] != input[1] || got[2] != input[2] || got[3] != wantI64 || got[4] != wantF32 || got[5] != wantF64 {
					t.Fatal("mutable host-reference-carrier return_call_ref", got, err)
				}
				rv64AssertPublicReferenceCarrierStateClear(t, consumer)
			}
			assertCall(0x80000001, ^uint64(0), uint64(math.Float32bits(2)), math.Float64bits(2.5))
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install tail host-reference-carrier transform", err)
			}
			assertCall(0x80000002, 0, uint64(math.Float32bits(3)), math.Float64bits(3.5))

			foreignCode := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-host-reference-carriers-foreign", rv64MutableFuncrefLocalCallHostReferenceCarrierModule(), artifact)
			foreign := rv64NativeTokenInstantiate(t, rt, foreignCode, NewImports())
			foreignDescriptor, ok := foreign.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign host-reference-carrier target descriptor")
			}
			foreignToken, err := rt.refStore.issue(foreign, foreignDescriptor)
			if err != nil {
				t.Fatal("foreign host-reference-carrier target token", err)
			}
			foreignTokenHeld := foreign.referenceLifetime().snapshot()
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, foreignToken)); err != nil {
				t.Fatal("install foreign host-reference-carrier tail target", err)
			}
			foreignInstalled := foreign.referenceLifetime().snapshot()
			if foreignInstalled.ResourceRoots != foreignTokenHeld.ResourceRoots+1 {
				t.Fatal("foreign host-reference-carrier tail cell root", foreignTokenHeld, foreignInstalled)
			}
			if err := foreign.Close(); err != nil {
				t.Fatal(err)
			}
			assertCall(0x80000001, ^uint64(0), uint64(math.Float32bits(2)), math.Float64bits(2.5))
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear foreign host-reference-carrier tail target", err)
			}
			if cleared := foreign.referenceLifetime().snapshot(); !cleared.LogicallyClosed || !cleared.PhysicalResources || cleared.ResourceRoots+1 != foreignInstalled.ResourceRoots {
				t.Fatal("foreign host-reference-carrier tail clear root", foreignInstalled, cleared)
			}

			calls := 0
			host := HostCallFunc(func(call HostCall) {
				calls++
				if uint32(call.I32(0)) != 0x80000001 || call.FuncRef(1).IsNull() || call.ExternRef(2) != ref || call.I64(3) != -1 || math.Float32bits(call.F32(4)) != math.Float32bits(2) || math.Float64bits(call.F64(5)) != math.Float64bits(2.5) {
					panic("mutable tail host-reference-carrier arguments")
				}
				call.ResultSlots()[0] = 0xfeedface00000000 | uint64(uint32(call.I32(0))+5)
				call.SetFuncRef(1, call.FuncRef(1))
				call.SetExternRef(2, call.ExternRef(2))
				call.SetI64(3, call.I64(3)+5)
				call.ResultSlots()[4] = 0xfeedface00000000 | uint64(math.Float32bits(call.F32(4)+5))
				call.SetF64(5, call.F64(5)+5)
			})
			types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-host-reference-carriers-producer", rv64PublicReferenceTailModule(types, true, true), artifact)
			owner, err := rt.NewHostFuncRef(host, FuncSig{Params: []ValType{ValI32, ValFuncRef, ValExternRef, ValI64, ValF32, ValF64}, Results: []ValType{ValI32, ValFuncRef, ValExternRef, ValI64, ValF32, ValF64}})
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			producer := rv64HostTailInstantiate(t, rt, producerCode, NewImports().Function("env", "target", owner))
			if !rv64PublicDynamicHostReferenceBoundary(producer.c) || len(producer.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
				t.Fatal("dynamic tail host-reference-carrier producer witness")
			}
			descriptor := uint64(offHeapSlicePtr(producer.funcRefDescs)) + coreruntime.FuncRefDescBytes
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("dynamic tail host-reference-carrier token", err)
			}
			tokenHeld := producer.referenceLifetime().snapshot()
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install dynamic tail host-reference-carrier target", err)
			}
			installed := producer.referenceLifetime().snapshot()
			if installed.ResourceRoots != tokenHeld.ResourceRoots+1 {
				t.Fatal("dynamic tail host-reference-carrier cell root", tokenHeld, installed)
			}
			assertCall(0x80000006, 4, uint64(math.Float32bits(7)), math.Float64bits(7.5))
			if calls != 1 {
				t.Fatal("dynamic tail host-reference-carrier call count", calls)
			}
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear dynamic tail host-reference-carrier target", err)
			}
			if cleared := producer.referenceLifetime().snapshot(); cleared.ResourceRoots+1 != installed.ResourceRoots {
				t.Fatal("dynamic tail host-reference-carrier clear root", installed, cleared)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("mutable tail host-reference-carrier externref remained pinned")
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := foreignCode.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalReferenceVectorCarriers(t *testing.T) {
	for _, tail := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("tail%t-artifact%t", tail, artifact), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				source := rv64MutableFuncrefLocalCallReferenceVectorCarrierModule()
				variant := 23
				if tail {
					source = rv64MutableFuncrefLocalTailCallReferenceVectorCarrierModule()
					variant = 24
				}
				compiled := rv64NativeTailCompile(t, fmt.Sprintf("mutable-funcref-local-reference-vector-carriers-tail%t", tail), source, artifact)
				view := compiled.executionView()
				if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != variant || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) {
					t.Fatal("reference-vector-carrier local-call witness missing")
				}
				m := rv64NativeTailModule(view)
				if tail {
					if m == nil || len(m.Plans) != 1 || !rv64MutableReferenceCarrierTailPlan(view, m.Plans[0]) {
						t.Fatal("reference-vector-carrier local-tail plan missing", m)
					}
				} else if m != nil {
					t.Fatal("ordinary reference-vector-carrier retained tail plan")
				}
				consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
				ref, err := rt.NewExternRef("mutable-reference-vector-carrier")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := consumer.Invoke("set_0"); err != nil {
					t.Fatal("install reference-vector identity", err)
				}
				cell, err := consumer.GlobalValue("cell")
				if err != nil || cell.FuncRef().IsNull() {
					t.Fatal("reference-vector argument token", cell, err)
				}
				argumentRoot, err := rt.NewFuncRefGlobal(cell.FuncRef(), false)
				if err != nil {
					t.Fatal("retain reference-vector funcref argument", err)
				}
				defer argumentRoot.Close()
				input := []uint64{0xdeadbeef80000001, cell.Bits(), ValueExternRef(ref).Bits(), 0x0123456789abcdef, 0xfedcba9876543210, math.Float64bits(2.5)}
				identity := []uint64{0x80000001, input[1], input[2], input[3], input[4], input[5]}
				transformed := []uint64{0x80000002, input[1], input[2], ^input[3], ^input[4], math.Float64bits(3.5)}
				assertCall := func(want []uint64) {
					t.Helper()
					got, err := consumer.Invoke("call", input...)
					if err != nil || !slices.Equal(got, want) {
						t.Fatal("mutable reference-vector call_ref", got, err, "want", want)
					}
					rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
				}
				assertCall(identity)
				if _, err := consumer.Invoke("set_1"); err != nil {
					t.Fatal("install reference-vector transform", err)
				}
				assertCall(transformed)

				producer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
				descriptor, ok := producer.localFuncrefDescriptor(0)
				if !ok {
					t.Fatal("foreign reference-vector target descriptor")
				}
				token, err := rt.refStore.issue(producer, descriptor)
				if err != nil {
					t.Fatal("foreign reference-vector target token", err)
				}
				before := producer.referenceLifetime().snapshot()
				if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
					t.Fatal("install foreign reference-vector target", err)
				}
				installed := producer.referenceLifetime().snapshot()
				if installed.ResourceRoots != before.ResourceRoots+1 {
					t.Fatal("foreign reference-vector cell root", before, installed)
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				assertCall(identity)
				if _, err := consumer.Invoke("clear"); err != nil {
					t.Fatal("clear reference-vector target", err)
				}
				cleared := producer.referenceLifetime().snapshot()
				if !cleared.LogicallyClosed || !cleared.PhysicalResources || cleared.ResourceRoots+1 != installed.ResourceRoots {
					t.Fatal("foreign reference-vector target root not retired", installed, cleared)
				}
				if got, invokeErr := consumer.Invoke("call", input...); got != nil {
					t.Fatal("cleared reference-vector call returned values", got, invokeErr)
				} else {
					wantTrap := coreruntime.TrapNullReference
					if tail {
						wantTrap = coreruntime.TrapIndirectOutOfBounds
					}
					var trap *coreruntime.TrapError
					if !errors.As(invokeErr, &trap) || trap.Code != wantTrap {
						t.Fatal("cleared reference-vector call trap", invokeErr)
					}
				}
				rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
				if !rt.ReleaseExternRef(ref) {
					t.Fatal("reference-vector externref remained pinned")
				}
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTailCallMixedVector(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-mixed-vector", rv64MutableFuncrefLocalTailCallMixedVectorModule(), artifact)
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalCall(view) || !rv64HasReferenceTail(view) {
				t.Fatal("mixed vector local-tail-call witness missing")
			}
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			input := []uint64{0xdeadbeef80000001, 0x0123456789abcdef, 0xfedcba9876543210, math.Float64bits(2.5)}
			identity := []uint64{0x80000001, input[1], input[2], input[3]}
			transformed := []uint64{0x80000002, ^input[1], ^input[2], math.Float64bits(3.5)}
			assertCall := func(want []uint64) {
				t.Helper()
				got, err := consumer.Invoke("call", input...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatal("mixed vector mutable return_call_ref", got, err, "want", want)
				}
			}
			if _, err := consumer.Invoke("set_0"); err != nil {
				t.Fatal("install mixed vector tail identity", err)
			}
			assertCall(identity)
			if _, err := consumer.Invoke("set_1"); err != nil {
				t.Fatal("install mixed vector tail transform", err)
			}
			assertCall(transformed)

			types := []wasm.ValType{wasm.I32, wasm.V128, wasm.F64}
			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-mixed-vector-producer", rv64PublicReferenceTailModule(types, false, true), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign mixed vector tail producer descriptor")
			}
			token, err := rt.refStore.issue(producer, descriptor)
			if err != nil {
				t.Fatal("foreign mixed vector tail producer token", err)
			}
			if err := consumer.SetGlobalValue("cell", ValueOf(ValFuncRef, token)); err != nil {
				t.Fatal("install foreign mixed vector tail target", err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			assertCall(identity)
			if _, err := consumer.Invoke("clear"); err != nil {
				t.Fatal("clear mixed vector tail target", err)
			}
			if got, invokeErr := consumer.Invoke("call", input...); got != nil {
				t.Fatal("cleared mixed vector return_call_ref returned a result", got, invokeErr)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(invokeErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("cleared mixed vector mutable return_call_ref trap", invokeErr)
				}
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallParkedClearRetainsTarget(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()

			producerCode := rv64NativeTailCompile(t, "mutable-funcref-local-call-parked-producer", rv64PublicReferenceFunctionCallBoundaryModule(51), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("foreign producer descriptor")
			}
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-parked", rv64MutableFuncrefLocalCallModule(), artifact)
			consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
			if err := consumer.ensureGuestLocalFuncrefGlobalOwner(0); err != nil {
				t.Fatal("install local cell owner", err)
			}
			if err := consumer.globalCells[0].copyGuestFuncrefDescriptor(consumer, descriptor); err != nil {
				t.Fatal("install raw foreign typed target", err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			parked := producer.referenceLifetime().snapshot()
			if !parked.LogicallyClosed || !parked.PhysicalResources || parked.ResourceRoots != 1 {
				t.Fatal("cell did not become the sole closed-producer root", parked)
			}

			// Model the portal sequence exactly: acquire targets while native
			// execution is leased, park by dropping that lease, and recursively
			// enter the consumer from the host side to clear the mutable cell.
			nativeExecutionMu.Lock()
			lease, err := railshotAcquireNativeTableTargets(consumer)
			nativeExecutionMu.Unlock()
			if err != nil {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("acquire outer mutable-call activation", err)
			}
			if active := producer.referenceLifetime().snapshot(); active.ResourceRoots != parked.ResourceRoots+1 {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("outer activation did not retain its call target", parked, active)
			}
			if _, err := consumer.Invoke("clear"); err != nil {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("callback reentry clear", err)
			}
			if suspended := producer.referenceLifetime().snapshot(); !suspended.PhysicalResources || suspended.ResourceRoots != 1 {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("parked activation lost its replaced call target", suspended)
			}
			nativeExecutionMu.Lock()
			railshotRefreshNativeTableTargets(lease)
			nativeExecutionMu.Unlock()
			if resumed := producer.referenceLifetime().snapshot(); !resumed.PhysicalResources || resumed.ResourceRoots != 1 {
				railshotReleaseNativeTableTargets(lease)
				t.Fatal("resumed activation lost its original call target", resumed)
			}
			railshotReleaseNativeTableTargets(lease)
			if retired := producer.referenceLifetime().snapshot(); !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
				t.Fatal("outer activation did not retire its replaced call target", retired)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallHostReentryClear(t *testing.T) {
	for _, tail := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("tail%t-artifact%t", tail, artifact), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rt.Close()

				consumerSource := rv64MutableFuncrefLocalCallI32Module()
				if tail {
					consumerSource = rv64MutableFuncrefLocalTailCallI32Module()
				}
				compiled := rv64NativeTailCompile(t, fmt.Sprintf("mutable-funcref-local-call-host-reentry-tail%t", tail), consumerSource, artifact)
				view := compiled.executionView()
				if !rv64MutableFuncrefGlobalLocalCall(view) || rv64HasReferenceTail(view) != tail {
					t.Fatal("mutable host reentry caller witness", tail)
				}
				consumer := rv64NativeTokenInstantiate(t, rt, compiled, NewImports())
				var producer *Instance
				callbackRoots := -1
				callbackPhysical := false
				calls := 0
				host := rv64HostTokenOwner(t, rt, ValI32, slotHostFunc(func(_ HostModule, params, results []uint64) {
					calls++
					if len(params) != 1 || len(results) != 1 || params[0] != 0x80000001 {
						panic("mutable call host transport")
					}
					if err := consumer.SetGlobalValue("cell", ValueFuncRef(NullFuncRef())); err != nil {
						panic(fmt.Sprintf("mutable call host reentry clear: %v", err))
					}
					snapshot := producer.referenceLifetime().snapshot()
					callbackRoots = snapshot.ResourceRoots
					callbackPhysical = snapshot.PhysicalResources
					results[0] = 0xfeedface00000000 | uint64(uint32(params[0])+5)
				}))

				// Reuse the qualified table-free host-reference producer: imported
				// function zero is materialized into immutable global zero, but no
				// public token is issued. The mutable cell is therefore the provider's
				// sole root before the outer activation starts.
				source := rv64ReferenceNoTableModule(false, false, false)
				types := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))
				imports := wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)))
				source = rv64ReplaceTableStageSection(t, source, 1, append(types, imports...))
				source = rv64ReplaceTableStageSection(t, source, 3, wasmtest.Section(3, []byte{2, 1, 1}))
				source = rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("isnull", 0, 1), wasmtest.ExportEntry("carrier", 0, 2), wasmtest.ExportEntry("reference", 3, 255), wasmtest.ExportEntry("first", 3, 0))))
				carrier := []byte{1, 1, 0x70, 0x23, 0xff, 1, 0x21, 0, 0x02, 0x70, 0x20, 0, 0x0c, 0, 0x0b, 0xd1, 0x0b}
				source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0xff, 1, 0xd1, 0x0b}), append(wasmtest.ULEB(uint32(len(carrier))), carrier...))))
				producerCode := rv64TableStageCompile(t, "mutable-funcref-local-call-host-producer", source, artifact)
				producer = rv64NativeTokenInstantiate(t, rt, producerCode, NewImports().Function("env", "target", host))
				descriptor := readGlobalObject(producer.globalCells[0], ValFuncRef)
				if descriptor == 0 {
					t.Fatal("host producer descriptor")
				}
				if err := consumer.ensureGuestLocalFuncrefGlobalOwner(0); err != nil {
					t.Fatal("install host cell owner", err)
				}
				if err := consumer.globalCells[0].copyGuestFuncrefDescriptor(consumer, descriptor); err != nil {
					t.Fatal("install raw host target", err)
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := producerCode.Close(); err != nil {
					t.Fatal(err)
				}
				before := producer.referenceLifetime().snapshot()
				if !before.LogicallyClosed || !before.PhysicalResources || before.ResourceRoots != 1 {
					t.Fatal("mutable cell did not solely retain host producer", before)
				}
				got, err := consumer.Invoke("call", 0xdeadbeef80000001)
				if err != nil || !slices.Equal(got, []uint64{0x80000006}) || calls != 1 {
					t.Fatal("mutable host call_ref", got, calls, err)
				}
				if callbackRoots != 1 || !callbackPhysical {
					t.Fatal("host reentry clear retired parked producer", callbackRoots, callbackPhysical)
				}
				if retired := producer.referenceLifetime().snapshot(); !retired.LogicallyClosed || retired.PhysicalResources || retired.ResourceRoots != 0 {
					t.Fatal("host target activation did not retire after resume", retired)
				}
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallAdmission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-admission", rv64MutableFuncrefLocalCallModule(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
		t.Fatal("base local-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-export": func(v *Compiled) {
			v.Exports = map[string]int{"target_0": 0, "target_1": 1, "set_0": 2, "set_1": 3, "clear": 4, "other": 5}
		},
		"global-export":    func(v *Compiled) { v.GlobalExports = map[string]int{"other": 0} },
		"immutable":        func(v *Compiled) { v.Globals[0].Mutable = false },
		"untyped-global":   func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Defined = false },
		"missing-call-abi": func(v *Compiled) { v.nativeReferenceCallABI = false },
		"missing-control":  func(v *Compiled) { v.nativeReferenceControlABI = false },
		"function-type-id": func(v *Compiled) { v.FuncTypeID = slices.Clone(v.FuncTypeID); v.FuncTypeID[5] ^= 1 },
		"entry":            func(v *Compiled) { v.Entry = []int{0, 608, 1216, 1936, 2656, 3316} },
		"feature":          func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTypedFunctionReferences },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Globals = slices.Clone(bad.Globals)
			bad.ValueTypes = slices.Clone(bad.ValueTypes)
			bad.Exports = maps.Clone(bad.Exports)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile local-call artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallReferenceCarrierAdmission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-reference-carrier-admission", rv64MutableFuncrefLocalCallReferenceCarrierModule(), false)
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != 19 || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) {
		t.Fatal("base reference-carrier local-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":   func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"escape": func(v *Compiled) { v.dynamicFuncrefEscape = false },
		"function-parameter": func(v *Compiled) {
			v.Funcs[5].Params = slices.Clone(v.Funcs[5].Params)
			v.Funcs[5].Params[1] = ValExternRef
		},
		"type-parameter": func(v *Compiled) {
			v.Types[0].Params = slices.Clone(v.Types[0].Params)
			v.Types[0].Params[1] = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
		},
		"type-result-order": func(v *Compiled) {
			v.Types[0].Results = slices.Clone(v.Types[0].Results)
			v.Types[0].Results[0], v.Types[0].Results[1] = v.Types[0].Results[1], v.Types[0].Results[0]
		},
		"function-type-id": func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"entry":            func(v *Compiled) { v.Entry[1] += 4 },
		"missing-multivalue": func(v *Compiled) {
			v.requiredFeatures &^= CoreFeatureMultiValue
		},
		"missing-reference-types": func(v *Compiled) {
			v.requiredFeatures &^= CoreFeatureReferenceTypes
		},
		"global-heap-type": func(v *Compiled) { v.ValueTypes[0].Ref.Heap.TypeIndex++ },
		"missing-call-abi": func(v *Compiled) { v.nativeReferenceCallABI = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *view
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Types = slices.Clone(bad.Types)
			bad.ValueTypes = slices.Clone(bad.ValueTypes)
			bad.Entry = slices.Clone(bad.Entry)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile reference-carrier local-call artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallHostReferenceCarrierAdmission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-host-reference-carrier-admission", rv64MutableFuncrefLocalCallHostReferenceCarrierModule(), false)
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != 21 || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) {
		t.Fatal("base host-reference-carrier local-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":   func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"escape": func(v *Compiled) { v.dynamicFuncrefEscape = false },
		"function-parameter": func(v *Compiled) {
			v.Funcs[5].Params = slices.Clone(v.Funcs[5].Params)
			v.Funcs[5].Params[4] = ValF64
		},
		"function-result": func(v *Compiled) {
			v.Funcs[1].Results = slices.Clone(v.Funcs[1].Results)
			v.Funcs[1].Results[3] = ValI32
		},
		"type-parameter": func(v *Compiled) {
			v.Types[0].Params = slices.Clone(v.Types[0].Params)
			v.Types[0].Params[2] = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}
		},
		"type-result-order": func(v *Compiled) {
			v.Types[0].Results = slices.Clone(v.Types[0].Results)
			v.Types[0].Results[4], v.Types[0].Results[5] = v.Types[0].Results[5], v.Types[0].Results[4]
		},
		"function-type-id":        func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"entry":                   func(v *Compiled) { v.Entry[1] += 4 },
		"missing-multivalue":      func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMultiValue },
		"missing-reference-types": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"global-heap-type":        func(v *Compiled) { v.ValueTypes[0].Ref.Heap.TypeIndex++ },
		"missing-call-abi":        func(v *Compiled) { v.nativeReferenceCallABI = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *view
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Types = slices.Clone(bad.Types)
			bad.ValueTypes = slices.Clone(bad.ValueTypes)
			bad.Entry = slices.Clone(bad.Entry)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile host-reference-carrier local-call artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTailCallReferenceCarrierAdmission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-reference-carrier-admission", rv64MutableFuncrefLocalTailCallReferenceCarrierModule(), false)
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != 20 || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) {
		t.Fatal("base reference-carrier local-tail-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":                 func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"missing-call-abi":     func(v *Compiled) { v.nativeReferenceCallABI = false },
		"missing-tail-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTailCall },
		"missing-plan":         func(v *Compiled) { rv64StoreNativeTailModule(v, nil) },
		"plan-kind":            func(v *Compiled) { rv64NativeTailModule(v).Plans[0].Kind = encoder.NativeTailIndirect },
		"plan-type":            func(v *Compiled) { rv64NativeTailModule(v).Plans[0].TypeIndex = 1 },
		"plan-function":        func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FunctionIndex = 1 },
		"plan-source":          func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourcePC++ },
		"plan-frame":           func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FrameBytes += 16 },
		"plan-source-base":     func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourceBase -= 8 },
		"plan-source-stride":   func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourceStride = 16 },
		"plan-reference-base":  func(v *Compiled) { rv64NativeTailModule(v).Plans[0].ReferenceBase -= 8 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := cloneCompiledMetadata(view)
			bad.validateMemo, bad.codeCache = &validateMemo{}, nil
			if m := rv64NativeTailModule(view); m != nil {
				rv64StoreNativeTailModule(bad, &encoder.NativeTailModule{Plans: slices.Clone(m.Plans)})
			}
			mutate(bad)
			if err := railshotValidateCompiled(bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile reference-carrier local-tail-call artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalTailCallHostReferenceCarrierAdmission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-tail-call-host-reference-carrier-admission", rv64MutableFuncrefLocalTailCallHostReferenceCarrierModule(), false)
	defer compiled.Close()
	view := compiled.executionView()
	m := rv64NativeTailModule(view)
	if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != 22 || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) || m == nil || len(m.Plans) != 1 || !rv64MutableReferenceCarrierTailPlan(view, m.Plans[0]) {
		t.Fatal("base host-reference-carrier local-tail-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code":                 func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"escape":               func(v *Compiled) { v.dynamicFuncrefEscape = false },
		"missing-call-abi":     func(v *Compiled) { v.nativeReferenceCallABI = false },
		"missing-tail-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTailCall },
		"function-parameter": func(v *Compiled) {
			v.Funcs[5].Params = slices.Clone(v.Funcs[5].Params)
			v.Funcs[5].Params[4] = ValF64
		},
		"type-result-order": func(v *Compiled) {
			v.Types[0].Results = slices.Clone(v.Types[0].Results)
			v.Types[0].Results[4], v.Types[0].Results[5] = v.Types[0].Results[5], v.Types[0].Results[4]
		},
		"missing-plan":        func(v *Compiled) { rv64StoreNativeTailModule(v, nil) },
		"plan-kind":           func(v *Compiled) { rv64NativeTailModule(v).Plans[0].Kind = encoder.NativeTailIndirect },
		"plan-type":           func(v *Compiled) { rv64NativeTailModule(v).Plans[0].TypeIndex = 1 },
		"plan-function":       func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FunctionIndex = 1 },
		"plan-source":         func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourcePC++ },
		"plan-frame":          func(v *Compiled) { rv64NativeTailModule(v).Plans[0].FrameBytes += 16 },
		"plan-source-base":    func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourceBase -= 8 },
		"plan-source-stride":  func(v *Compiled) { rv64NativeTailModule(v).Plans[0].SourceStride = 16 },
		"plan-reference-base": func(v *Compiled) { rv64NativeTailModule(v).Plans[0].ReferenceBase -= 8 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := cloneCompiledMetadata(view)
			bad.validateMemo, bad.codeCache = &validateMemo{}, nil
			if plan := rv64NativeTailModule(view); plan != nil {
				rv64StoreNativeTailModule(bad, &encoder.NativeTailModule{Plans: slices.Clone(plan.Plans)})
			}
			mutate(bad)
			if err := railshotValidateCompiled(bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile host-reference-carrier local-tail-call artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalReferenceVectorCarrierAdmission(t *testing.T) {
	for _, tail := range []bool{false, true} {
		t.Run(fmt.Sprintf("tail%t", tail), func(t *testing.T) {
			source := rv64MutableFuncrefLocalCallReferenceVectorCarrierModule()
			variant := 23
			if tail {
				source = rv64MutableFuncrefLocalTailCallReferenceVectorCarrierModule()
				variant = 24
			}
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-reference-vector-carrier-admission", source, false)
			defer compiled.Close()
			view := compiled.executionView()
			if !rv64MutableFuncrefGlobalLocalCall(view) || rv64MutableFuncrefGlobalLocalCallCodeVariant(view) != variant || !rv64MutableReferenceCarrierTarget(view, 0) || !rv64MutableReferenceCarrierTarget(view, 1) {
				t.Fatal("base reference-vector-carrier witness rejected")
			}
			for name, mutate := range map[string]func(*Compiled){
				"code":                 func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
				"missing-simd":         func(v *Compiled) { v.requiredFeatures &^= CoreFeatureSIMD },
				"missing-multivalue":   func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMultiValue },
				"missing-call-abi":     func(v *Compiled) { v.nativeReferenceCallABI = false },
				"function-vector":      func(v *Compiled) { v.Funcs[5].Params[3] = ValF64 },
				"declared-vector":      func(v *Compiled) { v.Types[0].Results[3] = ValueTypeDescriptor{Kind: ValueTypeF64} },
				"missing-tail-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTailCall },
				"plan-stride": func(v *Compiled) {
					if m := rv64NativeTailModule(v); m != nil {
						m.Plans[0].SourceStride = 8
					}
				},
				"plan-reference-base": func(v *Compiled) {
					if m := rv64NativeTailModule(v); m != nil {
						m.Plans[0].ReferenceBase -= 16
					}
				},
			} {
				t.Run(name, func(t *testing.T) {
					if !tail && (name == "missing-tail-feature" || name == "plan-stride" || name == "plan-reference-base") {
						return
					}
					bad := cloneCompiledMetadata(view)
					bad.validateMemo, bad.codeCache = &validateMemo{}, nil
					if m := rv64NativeTailModule(view); m != nil {
						rv64StoreNativeTailModule(bad, &encoder.NativeTailModule{Plans: slices.Clone(m.Plans)})
					}
					bad.Funcs = slices.Clone(bad.Funcs)
					bad.Funcs[5].Params = slices.Clone(bad.Funcs[5].Params)
					bad.Types = slices.Clone(bad.Types)
					bad.Types[0].Results = slices.Clone(bad.Types[0].Results)
					mutate(bad)
					if err := railshotValidateCompiled(bad); !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("hostile reference-vector-carrier artifact admitted", err)
					}
				})
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallI32Admission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-i32-admission", rv64MutableFuncrefLocalCallI32Module(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
		t.Fatal("base i32 local-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"parameter": func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[5].Params = slices.Clone(v.Funcs[5].Params)
			v.Funcs[5].Params[0] = ValI64
		},
		"type-parameter": func(v *Compiled) {
			v.Types = slices.Clone(v.Types)
			v.Types[0].Params = slices.Clone(v.Types[0].Params)
			v.Types[0].Params[0] = ValueTypeDescriptor{Kind: ValueTypeI64}
		},
		"entry": func(v *Compiled) { v.Entry = []int{0, 608, 1264, 1984, 2704, 3364} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Types = slices.Clone(bad.Types)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile i32 local-call artifact admitted", err)
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallScalarAdmission(t *testing.T) {
	modules := map[string][]byte{
		"i64": rv64MutableFuncrefLocalCallScalarModule(wasm.I64, []byte{0x20, 0, 0x42, 1, 0x7c, 0x0b}),
		"f32": rv64MutableFuncrefLocalCallScalarModule(wasm.F32, []byte{0x20, 0, 0x43, 0, 0, 0x80, 0x3f, 0x92, 0x0b}),
		"f64": rv64MutableFuncrefLocalCallScalarModule(wasm.F64, []byte{0x20, 0, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x0b}),
	}
	for name, source := range modules {
		t.Run(name, func(t *testing.T) {
			compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-"+name+"-admission", source, false)
			defer compiled.Close()
			if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
				t.Fatal("base scalar local-call witness rejected")
			}
			for mutation, mutate := range map[string]func(*Compiled){
				"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
				"parameter": func(v *Compiled) {
					v.Funcs = slices.Clone(v.Funcs)
					v.Funcs[5].Params = slices.Clone(v.Funcs[5].Params)
					v.Funcs[5].Params[0] = ValI32
				},
				"type-result": func(v *Compiled) {
					v.Types = slices.Clone(v.Types)
					v.Types[0].Results = slices.Clone(v.Types[0].Results)
					v.Types[0].Results[0] = ValueTypeDescriptor{Kind: ValueTypeI32}
				},
				"entry": func(v *Compiled) { v.Entry = slices.Clone(v.Entry); v.Entry[5] += 4 },
			} {
				t.Run(mutation, func(t *testing.T) {
					bad := *compiled.executionView()
					bad.Funcs = slices.Clone(bad.Funcs)
					bad.Types = slices.Clone(bad.Types)
					mutate(&bad)
					if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("hostile scalar local-call artifact admitted", err)
					}
				})
			}
		})
	}
}

func TestRV64PublicMutableFuncrefGlobalLocalCallMixedAdmission(t *testing.T) {
	compiled := rv64NativeTailCompile(t, "mutable-funcref-local-call-mixed-admission", rv64MutableFuncrefLocalCallMixedModule(), false)
	defer compiled.Close()
	if !rv64MutableFuncrefGlobalLocalCall(compiled.executionView()) {
		t.Fatal("base mixed local-call witness rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"function-parameter": func(v *Compiled) {
			v.Funcs = slices.Clone(v.Funcs)
			v.Funcs[5].Params = slices.Clone(v.Funcs[5].Params)
			v.Funcs[5].Params[2] = ValI32
		},
		"type-result": func(v *Compiled) {
			v.Types = slices.Clone(v.Types)
			v.Types[0].Results = slices.Clone(v.Types[0].Results)
			v.Types[0].Results[3] = ValueTypeDescriptor{Kind: ValueTypeI32}
		},
		"entry": func(v *Compiled) { v.Entry = slices.Clone(v.Entry); v.Entry[1] += 4 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *compiled.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Types = slices.Clone(bad.Types)
			mutate(&bad)
			if err := railshotValidateCompiled(&bad); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mixed local-call artifact admitted", err)
			}
		})
	}
}
