//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingExtendedRecursiveLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingExtendedRecursiveLinkWire("0061736d0100000001c780808000044e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e0250010260000164705001066000016406038380808000020405078d80808000020367313100000367313200010a918080800002838080800000000b838080800000000b"),
	mustDecodeGCTypeSubtypingExtendedRecursiveLinkWire("0061736d0100000001c780808000044e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e025001026000016470500106600001640602c98080800008024d39036731310000024d39036731310002024d39036731320000024d39036731320002024d39036731310004024d39036731310006024d39036731320005024d39036731320007"),
}

var rv64GCTypeSubtypingExtendedRecursiveLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingExtendedRecursiveLinkWires))
	for i, wire := range rv64GCTypeSubtypingExtendedRecursiveLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingExtendedRecursiveLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingExtendedRecursiveLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingExtendedRecursiveLinkWires {
		if bytes.Equal(data, wire) {
			if !admitExactProductWireShapeWithRecGroups(data, [][]byte{{1, 3, 7, 10}, {1, 2}}[i]) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingExtendedRecursiveLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}
