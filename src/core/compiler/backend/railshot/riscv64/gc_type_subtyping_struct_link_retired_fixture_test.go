//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingStructLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingStructLinkWire("0061736d01000000019780808000024e0250006000005f016400004e025001006000005f00038280808000010207858080800001016700000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingStructLinkWire("0061736d01000000019780808000024e0250006000005f016400004e025001006000005f0002888080800001024d3301670002"),
}

var rv64GCTypeSubtypingStructLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingStructLinkWires))
	for i, wire := range rv64GCTypeSubtypingStructLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingStructLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingStructLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingStructLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingStructLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}
