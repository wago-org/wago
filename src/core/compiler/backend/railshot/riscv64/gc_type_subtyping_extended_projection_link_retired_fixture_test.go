//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingExtendedProjectionLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingExtendedProjectionLinkWire("0061736d0100000001c380808000044e02500060000050005f016400004e02500060000050005f016402004e025001026000005001035f056400006402006400006402006404004e025001046000005f00038280808000010607858080800001016800000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingExtendedProjectionLinkWire("0061736d0100000001c380808000044e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001046000005f00028f8080800002024d3701680000024d3701680004"),
}

var rv64GCTypeSubtypingExtendedProjectionLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingExtendedProjectionLinkWires))
	for i, wire := range rv64GCTypeSubtypingExtendedProjectionLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingExtendedProjectionLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingExtendedProjectionLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingExtendedProjectionLinkWires {
		if bytes.Equal(data, wire) {
			if !admitExactProductWireShapeWithRecGroups(data, [][]byte{{1, 3, 7, 10}, {1, 2}}[i]) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingExtendedProjectionLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}
