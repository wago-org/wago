//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingFinalityLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingFinalityLinkWire("0061736d01000000018980808000025000600000600000038380808000020001078b8080800002026631000002663200010a8f80808000028280808000000b8280808000000b"),
	mustDecodeGCTypeSubtypingFinalityLinkWire("0061736d0100000001898080800002500060000060000002898080800001024d320266310001"),
	mustDecodeGCTypeSubtypingFinalityLinkWire("0061736d0100000001898080800002500060000060000002898080800001024d320266320000"),
}

var rv64GCTypeSubtypingFinalityLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingFinalityLinkWires))
	for i, wire := range rv64GCTypeSubtypingFinalityLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingFinalityLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingFinalityLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingFinalityLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingFinalityLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}
