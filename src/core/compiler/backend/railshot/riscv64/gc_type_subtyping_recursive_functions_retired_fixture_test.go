//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingRecursiveFunctionWires = [][]byte{
	mustDecodeGCTypeSubtypingRecursiveFunctionWire("0061736d0100000001ac80808000044e03500060027f64020050010060027f64010050010160027f640000600164000060016401006001640200038480808000030304050aae8080800003868080800000200010000b8a808080000020001000200010010b8e80808000002000100020001001200010020b"),
	mustDecodeGCTypeSubtypingRecursiveFunctionWire("0061736d0100000001c880808000054e0250006000027f640150006000027d64004e045001006000027f64055001016000027d64045001006000027f64035001016000027d6402600164000060016402006001640400038480808000030607080aaa8080800003868080800000200010000b8a808080000020001000200010010b8a808080000020001000200010020b"),
}

var rv64GCTypeSubtypingRecursiveFunctionModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingRecursiveFunctionWires))
	for i, wire := range rv64GCTypeSubtypingRecursiveFunctionWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingRecursiveFunctionWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingRecursiveFunctionsSourceVariant(data []byte) uint8 {
	if !admitExactProductWireShapeWithRecGroups(data, []byte{1, 3, 10}) {
		return 0
	}
	for i, wire := range rv64GCTypeSubtypingRecursiveFunctionWires {
		if bytes.Equal(data, wire) {
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingRecursiveFunctions(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}
