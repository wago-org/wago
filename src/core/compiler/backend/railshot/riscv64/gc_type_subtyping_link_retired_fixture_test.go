//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingLinkWire("0061736d01000000019780808000035000600001705001006000016301500101600001630203848080800003000102079080808000030266300000026631000102663200020a9c8080800003848080800000d0700b848080800000d0010b848080800000d0020b"),
	mustDecodeGCTypeSubtypingLinkWire("0061736d01000000019780808000035000600001705001006000016301500101600001630202ab8080800006014d0266300000014d0266310000014d0266310001014d0266320000014d0266320001014d0266320002"),
	mustDecodeGCTypeSubtypingLinkWire("0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266300001"),
	mustDecodeGCTypeSubtypingLinkWire("0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266300002"),
	mustDecodeGCTypeSubtypingLinkWire("0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266310002"),
}

var rv64GCTypeSubtypingLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingLinkWires))
	for i, wire := range rv64GCTypeSubtypingLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}, {1, 2}, {1, 2}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}
