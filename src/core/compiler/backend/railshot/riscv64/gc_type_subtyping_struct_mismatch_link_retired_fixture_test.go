//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingStructMismatchLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingStructMismatchLinkWire("0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016400004e025001026000005f00038280808000010407858080800001016700000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingStructMismatchLinkWire("0061736d01000000019780808000024e0250006000005f016400004e025001006000005f0002888080800001024d3501670002"),
}

var rv64GCTypeSubtypingStructMismatchLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingStructMismatchLinkWires))
	for i, wire := range rv64GCTypeSubtypingStructMismatchLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingStructMismatchLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingStructMismatchLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingStructMismatchLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingStructMismatchLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}
