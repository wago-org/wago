//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingIndependentStructLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingIndependentStructLinkWire("0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f00038280808000010407858080800001016700000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingIndependentStructLinkWire("0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f0002888080800001024d3601670000"),
}

var rv64GCTypeSubtypingIndependentStructLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingIndependentStructLinkWires))
	for i, wire := range rv64GCTypeSubtypingIndependentStructLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingIndependentStructLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingIndependentStructLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingIndependentStructLinkWires {
		if bytes.Equal(data, wire) {
			if !admitExactProductWireShapeWithRecGroups(data, [][]byte{{1, 3, 7, 10}, {1, 2}}[i]) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingIndependentStructLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}
